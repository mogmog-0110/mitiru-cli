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

	"github.com/spf13/cobra"
)

var (
	distOut  string
	distZip  bool
	distExe  bool
	distPack bool
)

// runtimeAllow は配布物の top-level に入れてよいファイル (allowlist)。DeployDir は
// cmake 出力 dir なので CMakeCache.txt / build.ninja / engine source / 他ツール exe
// 等が同居する。それらを持ち込まないよう、host + CEF runtime + DX のみを通す
// (build_windows_zip.py の RUNTIME_FILES と同集合)。
var runtimeAllow = map[string]bool{
	"mitiru_host.exe": true, "MitiruCefHelper.exe": true,
	"libcef.dll": true, "chrome_elf.dll": true,
	"d3dcompiler_47.dll": true, "dxcompiler.dll": true, "dxil.dll": true,
	"libEGL.dll": true, "libGLESv2.dll": true,
	"vk_swiftshader.dll": true, "vulkan-1.dll": true, "vk_swiftshader_icd.json": true,
	"chrome_100_percent.pak": true, "chrome_200_percent.pak": true, "resources.pak": true,
	"icudtl.dat": true, "snapshot_blob.bin": true, "v8_context_snapshot.bin": true,
}

// cefOnlyFiles は CEF (Chromium) 専用ファイル。mitiru.toml で [cef] enabled=false に
// した native ビルドでは、これらを外して ~30MB に収める (host + DX は残す)。
var cefOnlyFiles = map[string]bool{
	"libcef.dll": true, "chrome_elf.dll": true,
	"libEGL.dll": true, "libGLESv2.dll": true,
	"vk_swiftshader.dll": true, "vulkan-1.dll": true, "vk_swiftshader_icd.json": true,
	"chrome_100_percent.pak": true, "chrome_200_percent.pak": true, "resources.pak": true,
	"icudtl.dat": true, "snapshot_blob.bin": true, "v8_context_snapshot.bin": true,
	"MitiruCefHelper.exe": true,
}

// distJunkExt は配布物に含めない build linker 中間物。
var distJunkExt = map[string]bool{".ilk": true, ".pdb": true, ".exp": true, ".lib": true}

func newDistCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dist",
		Short: "Package the current project into a distributable folder",
		Long: `Build the project in Release and assemble a self-contained, runnable
bundle — the host, the engine runtime, your game DLL and assets, plus a
double-clickable launcher .bat.

If [cef] enabled=false in mitiru.toml, the Chromium (CEF) runtime is left
out, producing a much smaller native-only bundle.

A double-clickable launcher .bat is always written. With --exe, a <name>.exe
launcher is added as well (handy for Steam and other stores that expect an
.exe entry point).

Examples:
  mitiru dist                 # → dist/<name>/  (with <name>.bat)
  mitiru dist --exe           # also add <name>.exe launcher
  mitiru dist --zip           # also produce dist/<name>.zip
  mitiru dist --out build/ship`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDist()
		},
	}
	cmd.Flags().StringVar(&distOut, "out", "dist", "output directory for the bundle")
	cmd.Flags().BoolVar(&distZip, "zip", false, "also produce a .zip next to the bundle")
	cmd.Flags().BoolVar(&distExe, "exe", false,
		"also create <name>.exe launcher (double-click / Steam friendly)")
	cmd.Flags().BoolVar(&distPack, "pack", false,
		"embed assets/ into a single assets.mtpak (hide HTML/CSS/images/audio from the folder)")
	cmd.Flags().StringVar(&buildGenerator, "generator", "",
		"explicit CMake generator (default Ninja)")
	return cmd
}

func runDist() error {
	buildRelease = true // 配布は常に Release。
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

	noCef := cfg.CEF.Enabled != nil && !*cfg.CEF.Enabled
	gameDir := strings.SplitN(filepath.ToSlash(art.DllRel), "/", 2)[0]
	n, err := copyDeploy(art.DeployDir, bundleRoot, gameDir, noCef)
	if err != nil {
		return err
	}

	hostArgs := hostArgsFromConfig(cfg)
	batName := name + ".bat"
	if err := writeLauncher(filepath.Join(bundleRoot, batName), art.DllRel, hostArgs); err != nil {
		return err
	}
	n++

	if distExe {
		if err := writeExeLauncher(bundleRoot, name, art.DllRel, hostArgs); err != nil {
			return err
		}
		n++
	}

	if distPack {
		// <gameDir>/assets/ を <gameDir>/assets.mtpak に畳んで、バラ置きを除去する。
		// キーは host / native loader / CEF が要求する cwd 相対パス "<gameDir>/assets/..."。
		assetsDir := filepath.Join(bundleRoot, gameDir, "assets")
		if _, statErr := os.Stat(assetsDir); statErr == nil {
			packOut := filepath.Join(bundleRoot, gameDir, "assets.mtpak")
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

	mode := "HTML UI (CEF) 同梱"
	if noCef {
		mode = "ネイティブ (CEF なし)"
	}
	launch := batName
	if distExe {
		launch = name + ".exe / " + batName
	}
	fmt.Printf("\nDist OK: %s\n  %d files / %s\n  起動: %s をダブルクリック\n",
		bundleRoot, n, mode, launch)
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
// allowlist 方式: ゲーム dir (gameDir) は丸ごと、top-level は runtimeAllow のみ、
// locales/ は CEF 時のみ。それ以外 (CMakeCache 等の build 産物) は持ち込まない。
// native (noCef) 時は CEF/Chromium ファイルを除外する。コピー数を返す。
func copyDeploy(src, dst, gameDir string, noCef bool) (int, error) {
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
			// top-level dir は gameDir と (CEF 時の) locales だけ降りる。
			if !strings.Contains(rel, "/") {
				if rel == gameDir {
					return nil
				}
				if rel == "locales" && !noCef {
					return nil
				}
				return filepath.SkipDir
			}
			return nil
		}

		if distJunkExt[strings.ToLower(filepath.Ext(base))] {
			return nil
		}
		switch {
		case first == gameDir:
			// ゲーム dir 配下 (dll + assets + runtime) は全部入れる。
		case first == "locales":
			if noCef {
				return nil
			}
		case !strings.Contains(rel, "/"): // top-level ファイル
			if !runtimeAllow[base] || (noCef && cefOnlyFiles[base]) {
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
		"cd /d \"%~dp0\"\r\n" +
		"mitiru_host.exe " + args + "\r\n"
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
