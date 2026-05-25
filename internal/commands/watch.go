package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

// 変更されたら DLL rebuild を trigger すべき file 拡張子。
// それ以外 (例 assets/ 配下の .html, .css, .js) は engine 自身の asset hot reload が
// 拾うので、何も bounce する必要はない。
var cppExts = map[string]bool{
	".cpp": true, ".cc": true, ".cxx": true,
	".h": true, ".hpp": true, ".hxx": true,
	".inl": true,
}

func newWatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Run with L3 hot reload — rebuild on src/ change, host swaps the DLL live",
		Long: `'mitiru watch' is the editor loop. It builds and launches the game
once via mitiru_host --watch, then rebuilds on every src/ change. The
host detects the new DLL by mtime and reloads it in place — gameplay
state survives the swap (ADR 0005).

  src/**/*.{cpp,h,hpp,...} change → rebuild DLL → host hot-reloads
  assets/**/*.{html,css,js}      → engine's own hot reload picks it up

Press Ctrl-C to stop watching (also closes the game window). Saves
during a rebuild are coalesced so a burst of writes only triggers one
build.

With --inspect, also launches the sub-window inspector once alongside
the game.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch()
		},
	}
	cmd.Flags().BoolVar(&buildRelease, "release", false, "use Release configuration for rebuilds")
	cmd.Flags().StringVar(&buildConfigName, "config", "",
		"explicit CMake configuration (Debug|Release|RelWithDebInfo)")
	cmd.Flags().StringVar(&buildGenerator, "generator", "",
		"explicit CMake generator (default is Ninja)")
	cmd.Flags().BoolVar(&runWithInspect, "inspect", false,
		"also launch the sub-window inspector alongside the game")
	return cmd
}

func runWatch() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("watch: getwd: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch: create fsnotify watcher: %w", err)
	}
	defer watcher.Close()

	// src/ + assets/ を walk し、watcher が追う必要のある全 directory を追加する。
	for _, sub := range []string{"src", "assets"} {
		root := filepath.Join(cwd, sub)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		if err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				_ = watcher.Add(p)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("watch: walk %s: %w", root, err)
		}
	}

	fmt.Printf("mitiru watch: watching %s\n", cwd)
	fmt.Println("  ↳ src/**.{cpp,hpp,...}  →  rebuild DLL (host hot-reloads in place)")
	fmt.Println("  ↳ assets/**             →  engine hot-reload (no rebuild)")
	fmt.Println("  Ctrl-C to stop")

	state := newGameState()
	defer state.stop()

	// 初回 build + host を --watch で launch する。以降の DLL swap は host が内部で
	// すべて処理する — 後続の rebuild は disk 上の DLL を更新するだけでよく、
	// host は relaunch しない。
	if err := state.firstBuildAndLaunch(); err != nil {
		return fmt.Errorf("watch: initial build/launch failed: %w", err)
	}

	// build request は直列化された goroutine に集約され、save の burst が 1 回の
	// rebuild に collapse される。さらに長い build 中も fsnotify の Events channel が
	// drain され続ける。
	buildReq := make(chan struct{}, 1)
	go func() {
		for range buildReq {
			fmt.Println("\nmitiru watch: rebuilding...")
			if err := state.rebuildOnly(); err != nil {
				fmt.Fprintf(os.Stderr, "watch: rebuild failed: %v\n", err)
			}
		}
	}()
	requestBuild := func() {
		select {
		case buildReq <- struct{}{}:
		default:
			// 既に 1 件 queue 済み。worker は in-flight な build 完了後に
			// 最新の source を拾う。
		}
	}

	// Debounce: save event の burst を 1 回の rebuild request に coalesce する。
	var (
		debounceMu   sync.Mutex
		pendingTimer *time.Timer
	)
	schedule := func() {
		debounceMu.Lock()
		defer debounceMu.Unlock()
		if pendingTimer != nil { pendingTimer.Stop() }
		pendingTimer = time.AfterFunc(300*time.Millisecond, func() {
			requestBuild()
		})
	}

	for {
		select {
		case ev, ok := <-watcher.Events:
			if !ok { return nil }
			// 新規作成された subdirectory も watch する。
			if ev.Op&fsnotify.Create != 0 {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
					_ = watcher.Add(ev.Name)
				}
			}
			if !shouldTrigger(ev.Name, ev.Op) { continue }
			fmt.Printf("mitiru watch: %s changed\n", filepath.Base(ev.Name))
			schedule()
		case err, ok := <-watcher.Errors:
			if !ok { return nil }
			fmt.Fprintf(os.Stderr, "watch: %v\n", err)
		}
	}
}

func shouldTrigger(path string, op fsnotify.Op) bool {
	// write / create / rename のみに反応する。chmod だけは無視。
	if op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
		return false
	}
	// asset hot reload は engine 自身が面倒を見る。こちらは C++ 変更時のみ restart する。
	ext := strings.ToLower(filepath.Ext(path))
	if !cppExts[ext] { return false }
	// editor は気にしなくてよい temp / swap file をしばしば書き出す。
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") || strings.HasSuffix(base, "~") {
		return false
	}
	return true
}

// gameState は現在実行中のゲーム (と任意の inspector child) を保持する。
// 各 rebuild は新 process を spawn する前に旧 process を kill する。
type gameState struct {
	mu     sync.Mutex
	game   *exec.Cmd
	insp   *exec.Cmd
}

func newGameState() *gameState { return &gameState{} }

func (s *gameState) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insp != nil && s.insp.Process != nil {
		_ = s.insp.Process.Kill()
		_, _ = s.insp.Process.Wait()
		s.insp = nil
	}
	if s.game != nil && s.game.Process != nil {
		_ = s.game.Process.Kill()
		_, _ = s.game.Process.Wait()
		s.game = nil
	}
}

// firstBuildAndLaunch は初回 DLL build を行い、mitiru_host を --watch 付きで spawn
// する。後続の rebuild は host 自身の DLL mtime polling に依存するため、host process
// は `mitiru watch` の生存期間中ちょうど 1 回だけ launch される。
func (s *gameState) firstBuildAndLaunch() error {
	result, err := runBuild()
	if err != nil {
		return err
	}
	art := result.Artifacts

	fmt.Printf("\nLaunching %s %s --watch\n",
		filepath.Base(art.HostExePath), art.DllRel)

	// mitiru.toml の [window] サイズ / [font] atlas も host へ渡す (run と同じ)。
	watchArgs := append([]string{art.DllRel, "--watch"}, tomlHostArgs()...)
	cmd := exec.Command(art.HostExePath, watchArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = art.DeployDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %s: %w", art.HostExePath, err)
	}

	s.mu.Lock()
	s.game = cmd
	s.mu.Unlock()

	if runWithInspect {
		insp, err := startInspectorChild(cmd.Process.Pid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "watch: --inspect failed: %v\n", err)
		} else {
			s.mu.Lock()
			s.insp = insp
			s.mu.Unlock()
		}
	}
	return nil
}

// rebuildOnly は build を再実行する。host (firstBuildAndLaunch で 1 回 launch 済み)
// が DLL mtime を poll し in-place で reload する — gameplay state は swap を生き延びる。
func (s *gameState) rebuildOnly() error {
	_, err := runBuild()
	return err
}
