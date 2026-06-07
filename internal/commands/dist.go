package commands

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	distOut  string
	distZip  bool
	distExe  bool
	distBat  bool
	distPack bool
)

// distShipExe は top-level で配布してよい exe (host + CEF helper のみ)。他のツール exe
// (mitiru_inspector / mitiru_perf / mitiru_mixer / mitiru_replay / mitiru_scene_tree 等)
// は配布物に含めない。
var distShipExe = map[string]bool{
	"mitiru_host.exe": true, "MitiruCefHelper.exe": true,
}

// distJunkExt は配布物に含めない build linker 中間物。
var distJunkExt = map[string]bool{".ilk": true, ".pdb": true, ".exp": true, ".lib": true}

// isDistDropTopLevel は top-level ファイル (rel に "/" なし) を配布から外すか判定する。
// DeployDir は cmake 出力 dir なので CMakeCache.txt / build.ninja / *.cmake / 他ツール exe /
// build log 等が同居する。drop ルールに当たらないものは全て KEEP — 特に host が実際に
// import 依存する全 *.dll (vcpkg SDL2.dll 等) と CEF data (*.pak/*.dat/*.bin/*.json) を
// allowlist で取りこぼさないため、deny 方式に倒す。
func isDistDropTopLevel(base string) bool {
	low := strings.ToLower(base)
	ext := strings.ToLower(filepath.Ext(base))
	switch {
	case distJunkExt[ext]: // .ilk/.pdb/.exp/.lib
		return true
	case base == "CMakeCache.txt", base == "build.ninja", base == "cmake_install.cmake":
		return true
	case ext == ".cmake": // *.cmake (cmake 生成物)
		return true
	case strings.HasSuffix(low, ".ninja_log"), low == ".ninja_deps":
		return true // build log
	case base == "mitiru_start.exe":
		return true // ランチャ stub は data/ ではなくトップに置く (別途コピー)
	case ext == ".exe" && !distShipExe[base]:
		return true // host / CEF helper 以外の exe は配布しない
	default:
		return false
	}
}

func newDistCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dist",
		Short: "Package the current project into a distributable folder",
		Long: `Build the project in Release and assemble a self-contained, runnable
bundle — the host, the engine runtime, your game DLL and assets, plus a
double-clickable launcher .bat.

The top level holds a double-clickable <name>.exe launcher (a tiny GUI stub
that shows NO console window) plus README.txt; all runtime (host, DLLs, CEF,
your game, assets) lives in data/. Move/copy the whole folder as one unit.

Use --bat to also emit a console-visible <name>.bat (useful for reading logs
while debugging). --exe additionally drops a Steam-style data/<name>.exe.

Examples:
  mitiru dist                 # → dist/<name>/  (no-console <name>.exe)
  mitiru dist --bat           # also add a console-visible <name>.bat
  mitiru dist --zip           # also produce dist/<name>.zip
  mitiru dist --pack          # hide assets in a single assets.mtpak
  mitiru dist --out build/ship`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDist()
		},
	}
	cmd.Flags().StringVar(&distOut, "out", "dist", "output directory for the bundle")
	cmd.Flags().BoolVar(&distZip, "zip", false, "also produce a .zip next to the bundle")
	cmd.Flags().BoolVar(&distExe, "exe", false,
		"also copy <name>.exe (mitiru_host) into data/ as a Steam entry point")
	cmd.Flags().BoolVar(&distBat, "bat", false,
		"also write a console-visible <name>.bat launcher (handy for log/debug)")
	cmd.Flags().BoolVar(&distPack, "pack", false,
		"embed assets/ into a single assets.mtpak (hide HTML/CSS/images/audio from the folder)")
	cmd.Flags().StringVar(&buildGenerator, "generator", "",
		"explicit CMake generator (default Ninja)")
	return cmd
}

func runDist() error {
	// 配布物の性質を build 前に知るため manifest を先読みする (cef.enabled で no-cef ビルド)。
	cwd, _ := os.Getwd()
	mp, projectRoot, ferr := config.FindManifest(cwd)
	if ferr != nil {
		return ferr
	}
	pc, lerr := config.Load(mp)
	if lerr != nil {
		return lerr
	}
	noCef := pc.CEF.Enabled != nil && !*pc.CEF.Enabled

	// dist 専用ビルド: コンソール窓を出さない GUI host にする。dev の build/out を
	// 汚さないよう別 out dir (configure-time オプションの thrash 回避)。
	buildRelease = true
	buildOutDir = filepath.Join(projectRoot, "build", "dist-out")
	buildExtraDefines = []string{"MITIRU_HOST_GUI=ON"}
	if noCef {
		// native ([cef] enabled=false) は Chromium を一切 link / 同梱しない。
		// host は NullCefContext path でビルドされ libcef.dll を要求しない。
		buildExtraDefines = append(buildExtraDefines, "MITIRU_DISABLE_CEF=ON")
	}
	defer func() { buildOutDir = ""; buildExtraDefines = nil }() // 後続コマンドへ漏らさない

	result, err := runBuild()
	if err != nil {
		return err
	}
	cfg, art := result.Config, result.Artifacts

	name := distBundleName(cfg.Project.Name)
	bundleRoot, err := filepath.Abs(filepath.Join(distOut, name))
	if err != nil {
		return fmt.Errorf("dist: resolve out dir: %w", err)
	}
	if err := os.RemoveAll(bundleRoot); err != nil {
		return fmt.Errorf("dist: clear %s: %w", bundleRoot, err)
	}
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		return fmt.Errorf("dist: mkdir %s: %w", bundleRoot, err)
	}

	gameDir := strings.SplitN(filepath.ToSlash(art.DllRel), "/", 2)[0]

	// ランタイム一式 (host + 全 DLL + CEF + ゲーム) は data/ サブフォルダに隔離し、
	// トップ階層はランチャーだけにする (DLL の散らかりを隠す)。host は自分の exe dir
	// (= data/) を cwd に固定するので、data/ 内で全パスが完結する。
	dataDir := filepath.Join(bundleRoot, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("dist: mkdir data: %w", err)
	}
	n, err := copyDeploy(art.DeployDir, dataDir, gameDir)
	if err != nil {
		return err
	}

	hostArgs := hostArgsFromConfig(cfg)

	// 既定ランチャ: トップ階層の <name>.exe = GUI stub (mitiru_start)。コンソール窓を
	// 一切出さずに data\mitiru_host.exe を起動する。stub は data\launch.mtargs から
	// host への argv を読む (cwd=data 相対)。
	launchArgs := filepath.ToSlash(art.DllRel)
	if len(hostArgs) > 0 {
		launchArgs += " " + strings.Join(hostArgs, " ")
	}
	stubSrc := filepath.Join(art.DeployDir, "mitiru_start.exe")
	stubUsed := false
	if _, statErr := os.Stat(stubSrc); statErr == nil {
		if err := copyFile(stubSrc, filepath.Join(bundleRoot, name+".exe")); err != nil {
			return fmt.Errorf("dist: copy launcher stub: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, "launch.mtargs"),
			[]byte(launchArgs+"\n"), 0o644); err != nil {
			return fmt.Errorf("dist: write launch.mtargs: %w", err)
		}
		stubUsed = true
		n += 2
	}

	// stub が無い (古い engine / 非 Windows) ときは .bat にフォールバック。
	batName := name + ".bat"
	writeBat := distBat || !stubUsed
	if writeBat {
		if err := writeLauncher(filepath.Join(bundleRoot, batName), art.DllRel, hostArgs); err != nil {
			return err
		}
		n++
	}

	if distExe {
		// Steam 等の .exe 起点向けに、host を data/<name>.exe としても置く (sidecar mtargs)。
		if err := writeExeLauncher(dataDir, name, art.DllRel, hostArgs); err != nil {
			return err
		}
		n++
	}

	if distPack {
		// <gameDir>/assets/ を <gameDir>/assets.mtpak に畳んで、バラ置きを除去する。
		// キーは host / native loader / CEF が要求する cwd 相対パス "<gameDir>/assets/..."。
		assetsDir := filepath.Join(dataDir, gameDir, "assets")
		if _, statErr := os.Stat(assetsDir); statErr == nil {
			packOut := filepath.Join(dataDir, gameDir, "assets.mtpak")
			cnt, perr := packAssets(assetsDir, packOut, gameDir+"/assets")
			if perr != nil {
				return fmt.Errorf("dist --pack: %w", perr)
			}
			if rmErr := os.RemoveAll(assetsDir); rmErr != nil {
				return fmt.Errorf("dist --pack: remove loose assets: %w", rmErr)
			}
			fmt.Printf("Packed %d assets → %s (loose assets/ removed)\n", cnt, packOut)
		} else {
			fmt.Println("dist --pack: no assets/ to pack (skipped)")
		}
	}

	if distZip {
		zipPath := bundleRoot + ".zip"
		if err := zipDir(bundleRoot, filepath.Dir(bundleRoot), zipPath); err != nil {
			return err
		}
		fmt.Printf("Zipped: %s\n", zipPath)
	}

	// 起動方法を決める (README / 最終メッセージ共通)。
	primary := batName
	if stubUsed {
		primary = name + ".exe"
	}

	// トップに README を置き、構成を 1 行で説明する (中身は data/)。
	readme := name + " — MitiruEngine game\r\n\r\n" +
		primary + " をダブルクリックで起動。\r\n" +
		"data/ にランタイム一式が入っています (移動・削除しないでください)。\r\n"
	if err := os.WriteFile(filepath.Join(bundleRoot, "README.txt"), []byte(readme), 0o644); err != nil {
		return err
	}
	n++

	mode := "HTML UI (CEF) 同梱"
	if noCef {
		mode = "native 描画 (Chromium 非同梱)"
	}
	launch := primary
	if stubUsed {
		launch = primary + " (コンソール窓なし)"
		if writeBat {
			launch += " / " + batName
		}
	}
	fmt.Printf("\nDist OK: %s\n  %d files / %s\n  トップは %s + README + data/ のみ\n  起動: %s\n",
		bundleRoot, n, mode, primary, launch)
	return nil
}

// writeExeLauncher は mitiru_host.exe を <name>.exe にコピーし、引数なしで
// 起動されたとき読まれる sidecar <name>.mtargs を書く。host は引数なし起動時に
// この .mtargs を argv として読む (Steam 等の .exe 起点に対応)。
func writeExeLauncher(bundleRoot, name, dllRel string, hostArgs []string) error {
	host := filepath.Join(bundleRoot, "mitiru_host.exe")
	if _, err := os.Stat(host); err != nil {
		return fmt.Errorf("dist --exe: mitiru_host.exe not found in bundle: %w", err)
	}
	if err := copyFile(host, filepath.Join(bundleRoot, name+".exe")); err != nil {
		return fmt.Errorf("dist --exe: copy host: %w", err)
	}
	args := filepath.ToSlash(dllRel)
	if len(hostArgs) > 0 {
		args += " " + strings.Join(hostArgs, " ")
	}
	return os.WriteFile(filepath.Join(bundleRoot, name+".mtargs"), []byte(args+"\n"), 0o644)
}

// distBundleName は配布フォルダ名を ASCII-safe にする (zip / bat 名で安全)。
func distBundleName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z',
			r >= '0' && r <= '9', r == '_', r == '-':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	s := strings.Trim(string(out), "_")
	if s == "" {
		s = "game"
	}
	return s
}

// copyDeploy は DeployDir から配布に必要なものだけを bundle へコピーする。
// ゲーム dir (gameDir) と locales/ は丸ごと、top-level は deny 方式 (isDistDropTopLevel
// に当たらないものは全て KEEP) で全 runtime dll / CEF data を取りこぼさない。
//
// 注: `[cef] enabled=false` の場合、dist は MITIRU_DISABLE_CEF=ON で host を再ビルド
// する (runDist 冒頭) ため deploy dir に CEF runtime がそもそも現れない。ここの deny
// 方式は「ビルド出力に在るものは要る」前提でよく、CEF の有無をここで判定しない。
func copyDeploy(src, dst, gameDir string) (int, error) {
	count := 0
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		first := strings.SplitN(rel, "/", 2)[0]
		base := info.Name()

		if info.IsDir() {
			if strings.HasPrefix(base, "cef_cache_") || base == "CMakeFiles" || base == "__pycache__" {
				return filepath.SkipDir
			}
			// top-level dir は gameDir と locales だけ降りる。
			if !strings.Contains(rel, "/") && rel != gameDir && rel != "locales" {
				return filepath.SkipDir
			}
			return nil
		}

		if distJunkExt[strings.ToLower(filepath.Ext(base))] {
			return nil
		}
		switch {
		case first == gameDir, first == "locales":
			// ゲーム dir 配下 / locales は入れる。
		case !strings.Contains(rel, "/"): // top-level ファイル: drop ルールに当たるものだけ除外
			if isDistDropTopLevel(base) {
				return nil
			}
		default:
			return nil
		}
		if err := copyFile(path, filepath.Join(dst, rel)); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// writeLauncher は host を game DLL + host 引数で起動する .bat を書く。
func writeLauncher(path, dllRel string, hostArgs []string) error {
	args := dllRel
	if len(hostArgs) > 0 {
		args += " " + strings.Join(hostArgs, " ")
	}
	body := "@echo off\r\n" +
		"rem MitiruEngine game launcher\r\n" +
		"cd /d \"%~dp0data\"\r\n" + // ランタイムは data/ に隔離されている
		"mitiru_host.exe " + args + "\r\n" +
		"if errorlevel 1 pause\r\n" + // 起動失敗時はエラーを読めるよう留める (一瞬で消えない)
		""
	return os.WriteFile(path, []byte(body), 0o644)
}

// zipDir は root 以下を、base からの相対パスを arcname にして zip 化する
// (展開すると <name>/ フォルダが現れる)。
func zipDir(root, base, zipPath string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return relErr
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
}

// packAssets は assetsDir 以下を再帰的に読み、keyPrefix を前置したキーで .mtpak に
// 書き出す。キーは host / native loader / CEF が要求する cwd 相対パスに一致させる。
func packAssets(assetsDir, outFile, keyPrefix string) (int, error) {
	var keys []string
	var datas [][]byte
	err := filepath.Walk(assetsDir, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(assetsDir, path)
		if rerr != nil {
			return rerr
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		keys = append(keys, keyPrefix+"/"+filepath.ToSlash(rel))
		datas = append(datas, data)
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := writeAssetPack(outFile, keys, datas, true); err != nil {
		return 0, err
	}
	return len(keys), nil
}

// writeAssetPack は AssetPack.hpp (ADR 0016) と **バイト互換**の .mtpak を書く。
// 形式: magic"MTPAK\0" | version u16 | flags u16 | count u32 |
//       [count] keyLen u16, key, offset u64, size u64 | blob region (scramble 時 XOR)。
// C++ 側 (mitiru::vfs::AssetPack::open/read) がこれを読むので、両者の形式は一致必須。
func writeAssetPack(outFile string, keys []string, datas [][]byte, scramble bool) error {
	blobStart := uint64(6 + 2 + 2 + 4)
	for _, k := range keys {
		blobStart += uint64(2 + len(k) + 8 + 8)
	}
	var buf bytes.Buffer
	buf.WriteString("MTPAK\x00")
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // version
	var flags uint16
	if scramble {
		flags = 1
	}
	_ = binary.Write(&buf, binary.LittleEndian, flags)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(keys)))
	off := blobStart
	offsets := make([]uint64, len(keys))
	for i, k := range keys {
		_ = binary.Write(&buf, binary.LittleEndian, uint16(len(k)))
		buf.WriteString(k)
		_ = binary.Write(&buf, binary.LittleEndian, off)
		_ = binary.Write(&buf, binary.LittleEndian, uint64(len(datas[i])))
		offsets[i] = off
		off += uint64(len(datas[i]))
	}
	for i := range keys {
		d := datas[i]
		if scramble {
			d = make([]byte, len(datas[i]))
			copy(d, datas[i])
			for j := range d {
				d[j] ^= byte(0x5A + ((offsets[i] + uint64(j)) & 0xFF))
			}
		}
		buf.Write(d)
	}
	return os.WriteFile(outFile, buf.Bytes(), 0o644)
}
