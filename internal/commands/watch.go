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

// File extensions that should trigger a full rebuild + restart.
// Anything else (e.g. .html, .css, .js under assets/) is picked up by the
// engine's own asset hot-reload — no need for us to bounce the process.
var cppExts = map[string]bool{
	".cpp": true, ".cc": true, ".cxx": true,
	".h": true, ".hpp": true, ".hxx": true,
	".inl": true,
}

func newWatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch src/ for changes; auto-rebuild and re-launch the game",
		Long: `'mitiru watch' is the editor-loop helper. It watches the current
project's source tree and, when a C++ file changes, rebuilds and
re-launches the game automatically — no need to alt-tab to a terminal,
type 'mitiru run', and wait.

  src/**/*.{cpp,h,hpp,...} change → rebuild + restart game
  assets/**/*.{html,css,js}      → engine's own hot reload handles it
                                   (no restart needed)

Press Ctrl-C to stop watching. Changes during a rebuild are coalesced
so a burst of saves only triggers one rebuild.

With --inspect, also launches the sub-window inspector alongside each
restart and shuts it down before relaunch.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch()
		},
	}
	cmd.Flags().BoolVar(&buildRelease, "release", false, "use Release configuration for rebuilds")
	cmd.Flags().StringVar(&buildConfigName, "config", "",
		"explicit CMake configuration (Debug|Release|RelWithDebInfo)")
	cmd.Flags().StringVar(&buildGenerator, "generator", "",
		"explicit CMake generator (default is NMake Makefiles)")
	cmd.Flags().BoolVar(&runWithInspect, "inspect", false,
		"also launch the sub-window inspector for each game instance")
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

	// Walk src/ + assets/ and add every directory the watcher needs to follow.
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
	fmt.Println("  ↳ src/**.{cpp,hpp,...}  →  rebuild + restart")
	fmt.Println("  ↳ assets/**             →  engine hot-reload (no restart)")
	fmt.Println("  Ctrl-C to stop")

	state := newGameState()
	defer state.stop()

	// Build requests are funnelled through a serialised goroutine so:
	//  (a) the fsnotify Events channel keeps draining during long rebuilds —
	//      otherwise events fired while a build runs get dropped.
	//  (b) overlapping requests collapse into "build once more after the
	//      current one finishes" instead of stacking.
	buildReq := make(chan struct{}, 1)
	go func() {
		for range buildReq {
			fmt.Println("\nmitiru watch: building...")
			if err := state.rebuildAndRelaunch(); err != nil {
				fmt.Fprintf(os.Stderr, "watch: build failed: %v\n", err)
			}
		}
	}()
	requestBuild := func() {
		select {
		case buildReq <- struct{}{}:
		default:
			// One is already queued; the worker will see the latest source
			// after it finishes the in-flight build.
		}
	}

	// Kick off the initial build immediately so the user sees the running
	// state right away. We don't block on it — events fired during the build
	// still reach us via the goroutine + queued-build pattern above.
	requestBuild()

	// Debounce: coalesce a burst of save events into one rebuild request.
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
			// Watch newly-created subdirectories too.
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
	// Only react to writes / creates / renames; chmod alone doesn't matter.
	if op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
		return false
	}
	// Engine takes care of asset hot reload itself; we only restart on C++.
	ext := strings.ToLower(filepath.Ext(path))
	if !cppExts[ext] { return false }
	// Editors often write temp / swap files we don't care about.
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") || strings.HasSuffix(base, "~") {
		return false
	}
	return true
}

// gameState holds the currently-running game (and optional inspector child).
// Each rebuild kills the old process before spawning the new one.
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

func (s *gameState) rebuildAndRelaunch() error {
	s.stop()

	result, err := runBuild()
	if err != nil {
		return err
	}

	fmt.Printf("\nRunning %s\n", result.ExePath)
	cmd := exec.Command(result.ExePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin  = os.Stdin
	cmd.Dir    = result.ProjectRoot
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %s: %w", result.ExePath, err)
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
