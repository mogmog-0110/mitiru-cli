package commands

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/build"
)

// vcRuntimeDLLs は VC++ 再頒布パッケージの DLL。開発機には System32 にあるが、
// 何も入れていない PC には無いので、配布物の data/ に置く (app-local)。
var vcRuntimeDLLs = map[string]bool{
	"vcruntime140.dll": true, "vcruntime140_1.dll": true,
	"msvcp140.dll": true, "msvcp140_1.dll": true, "msvcp140_2.dll": true,
	"msvcp140_atomic_wait.dll": true, "msvcp140_codecvt_ids.dll": true,
	"concrt140.dll": true, "vcomp140.dll": true,
}

// mustBundleDLLs は開発機の System32 に入っていることがあっても、配布物に同梱しないといけない DLL。
// Windows 11 は自前の onnxruntime.dll / DirectML.dll を System32 に持つが版が合わない。
var mustBundleDLLs = map[string]bool{
	"onnxruntime.dll": true, "onnxruntime_providers_shared.dll": true, "directml.dll": true,
	"sdl3.dll": true, "sdl2.dll": true, "steam_api64.dll": true, "phonon.dll": true,
	"dxcompiler.dll": true, "dxil.dll": true, "vulkan-1.dll": true, "openal32.dll": true,
}

// isDebugCRT は再頒布できない Debug 版ランタイムかを返す。
func isDebugCRT(name string) bool {
	low := strings.ToLower(name)
	if low == "ucrtbased.dll" {
		return true
	}
	base := strings.TrimSuffix(low, ".dll")
	for rt := range vcRuntimeDLLs {
		r := strings.TrimSuffix(rt, ".dll")
		if base == r+"d" || strings.HasPrefix(base, r+"d_") {
			return true
		}
	}
	// msvcp140d_atomic_wait / msvcp140d_codecvt_ids は d が途中に入る
	return strings.HasPrefix(base, "msvcp140d")
}

// importProblem は配布物の 1 つの PE が読み込めない DLL を 1 件表す。
type importProblem struct {
	Importer string // data/ からの相対
	DLL      string
	Reason   string
}

// distImportReport は data/ 以下の全 PE の依存を調べた結果。
type distImportReport struct {
	MissingVC []string        // 同梱すれば直る VC ランタイム
	DebugCRT  []importProblem // Debug 版ランタイムへの依存
	Missing   []importProblem // それ以外で見つからない DLL
}

// peImportsFunc は PE が通常の import で読む DLL 名を返す (遅延読み込みは含まない)。
type peImportsFunc func(path string) ([]string, error)

func readPEImports(path string) ([]string, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Go の標準ライブラリは ImportedLibraries が未実装 (常に空) で、ImportedSymbols は序数で読む関数を
	// 落とす (onnxruntime.dll がそれで消える)。import 表の記述子から DLL 名を直に読む。
	var dir pe.DataDirectory
	switch oh := f.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		if len(oh.DataDirectory) > pe.IMAGE_DIRECTORY_ENTRY_IMPORT {
			dir = oh.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT]
		}
	case *pe.OptionalHeader32:
		if len(oh.DataDirectory) > pe.IMAGE_DIRECTORY_ENTRY_IMPORT {
			dir = oh.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT]
		}
	}
	var libs []string
	for desc := dir.VirtualAddress; dir.VirtualAddress != 0; desc += 20 {
		b, err := peReadRVA(f, desc, 20)
		if err != nil {
			return nil, err
		}
		nameRVA := binary.LittleEndian.Uint32(b[12:16])
		if nameRVA == 0 {
			break // 終わりの記述子
		}
		name, err := peReadRVA(f, nameRVA, 0)
		if err != nil {
			return nil, err
		}
		libs = append(libs, string(name))
	}
	return libs, nil
}

// peReadRVA は rva から n バイト (n == 0 なら NUL まで) を読む。
func peReadRVA(f *pe.File, rva uint32, n int) ([]byte, error) {
	for _, s := range f.Sections {
		if rva < s.VirtualAddress || rva >= s.VirtualAddress+s.VirtualSize {
			continue
		}
		data, err := s.Data()
		if err != nil {
			return nil, err
		}
		off := int(rva - s.VirtualAddress)
		if off >= len(data) {
			break
		}
		if n == 0 {
			if end := bytes.IndexByte(data[off:], 0); end >= 0 {
				return data[off : off+end], nil
			}
			return data[off:], nil
		}
		if off+n > len(data) {
			break
		}
		return data[off : off+n], nil
	}
	return nil, fmt.Errorf("RVA 0x%x がどの section にも無い", rva)
}

// systemDLLExists は素の Windows にも入っている DLL かを返す。
type systemDLLFunc func(name string) bool

func systemDLLInWindows(name string) bool {
	low := strings.ToLower(name)
	if strings.HasPrefix(low, "api-ms-win-") || strings.HasPrefix(low, "ext-ms-") {
		return true // UCRT と API set は Windows 10 以降の OS の一部
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	_, err := os.Stat(filepath.Join(root, "System32", name))
	return err == nil
}

// checkDistImports は dataDir 以下の .exe / .dll が読む DLL を、同梱物か素の Windows で満たせるか調べる。
// DLL の探索は exe の隣 (data/) と、読み込む側と同じフォルダで足りるかを見る。
func checkDistImports(dataDir string, imports peImportsFunc, system systemDLLFunc) (distImportReport, error) {
	var rep distImportReport
	present, pes, err := scanDistPE(dataDir)
	if err != nil {
		return rep, err
	}
	missingVC := map[string]bool{}
	for _, rel := range pes {
		libs, ierr := imports(filepath.Join(dataDir, filepath.FromSlash(rel)))
		if ierr != nil {
			return rep, fmt.Errorf("dist: %s の import を読めない: %w", rel, ierr)
		}
		dir := strings.ToLower(filepath.ToSlash(filepath.Dir(rel)))
		for _, lib := range libs {
			low := strings.ToLower(lib)
			if present[low] || present[dir+"/"+low] {
				continue
			}
			switch {
			case isDebugCRT(low):
				rep.DebugCRT = append(rep.DebugCRT, importProblem{rel, lib, "Debug 版ランタイム (再頒布できない)"})
			case vcRuntimeDLLs[low]:
				missingVC[low] = true
			case mustBundleDLLs[low]:
				rep.Missing = append(rep.Missing, importProblem{rel, lib, "配布物に無い (Windows に同名があっても版が合わない)"})
			case !system(lib):
				rep.Missing = append(rep.Missing, importProblem{rel, lib, "配布物にも Windows にも無い"})
			}
		}
	}
	for name := range missingVC {
		rep.MissingVC = append(rep.MissingVC, name)
	}
	sort.Strings(rep.MissingVC)
	return rep, nil
}

// scanDistPE は dataDir 以下のファイル名の集合 (data/ 直下は名前だけ、他は "dir/name") と、
// PE (.exe / .dll) の相対パスを返す。
func scanDistPE(dataDir string) (map[string]bool, []string, error) {
	present := map[string]bool{}
	var pes []string
	err := filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, rerr := filepath.Rel(dataDir, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		low := strings.ToLower(rel)
		present[low] = true
		ext := strings.ToLower(filepath.Ext(rel))
		if ext == ".exe" || ext == ".dll" {
			pes = append(pes, rel)
		}
		return nil
	})
	sort.Strings(pes)
	return present, pes, err
}

// vcRedistDirs は VC ランタイムの再頒布 DLL があるフォルダを返す (新しい版が先)。
// debug が true なら再頒布できない Debug 版 (debug_nonredist) を返す。
func vcRedistDirs(debug bool) []string {
	var roots []string
	if env := os.Getenv("VCToolsRedistDir"); env != "" {
		roots = append(roots, env)
	}
	if vcvars, err := build.FindVcvars64(); err == nil {
		vc := filepath.Dir(filepath.Dir(filepath.Dir(vcvars))) // ...\VC\Auxiliary\Build → ...\VC
		found, _ := filepath.Glob(filepath.Join(vc, "Redist", "MSVC", "*"))
		sort.Sort(sort.Reverse(sort.StringSlice(found)))
		roots = append(roots, found...)
	}
	var dirs []string
	for _, r := range roots {
		pattern := filepath.Join(r, "x64", "Microsoft.VC14*.*")
		if debug {
			pattern = filepath.Join(r, "debug_nonredist", "x64", "Microsoft.VC14*.Debug*")
		}
		m, _ := filepath.Glob(pattern)
		dirs = append(dirs, m...)
	}
	return dirs
}

// copyFromDirs は name を dirs の最初に見つかった場所から dst へ写す。
func copyFromDirs(name string, dirs []string, dst string) error {
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.EqualFold(e.Name(), name) {
				return copyFile(filepath.Join(d, e.Name()), filepath.Join(dst, strings.ToLower(name)))
			}
		}
	}
	return fmt.Errorf("%s が Visual Studio の Redist に見つからない", name)
}

// ensureDistRuntime は配布物の依存を調べ、VC ランタイムを data/ に置き、直せない欠けをエラーにする。
// debug は --debug の配布。そのときだけ Debug 版ランタイムを許し、同梱する。同梱した DLL の数を返す。
func ensureDistRuntime(dataDir string, debug bool) (int, error) {
	rep, err := checkDistImports(dataDir, readPEImports, systemDLLInWindows)
	if err != nil {
		return 0, err
	}
	redist := vcRedistDirs(false)
	for _, name := range rep.MissingVC {
		if err := copyFromDirs(name, redist, dataDir); err != nil {
			return 0, fmt.Errorf("dist: VC ランタイムを同梱できない: %w (Visual Studio の C++ 再頒布ファイルを確かめる)", err)
		}
	}
	if len(rep.DebugCRT) > 0 && !debug {
		return 0, fmt.Errorf("dist: Release の配布物が Debug 版ランタイムを読む。build/dist-out を消して作り直す\n%s",
			formatImportProblems(rep.DebugCRT))
	}
	debugDLLs := 0
	if debug {
		if debugDLLs, err = bundleDebugCRT(dataDir, rep.DebugCRT); err != nil {
			return 0, err
		}
	}
	if len(rep.Missing) > 0 {
		return 0, fmt.Errorf("dist: 配布物に足りない DLL がある (このままでは起動しない)\n%s",
			formatImportProblems(rep.Missing))
	}
	if len(rep.MissingVC) > 0 {
		fmt.Printf("dist: VC ランタイムを同梱した (%s)\n", strings.Join(rep.MissingVC, ", "))
	}
	return len(rep.MissingVC) + debugDLLs, nil
}

// bundleDebugCRT は --debug の配布に Debug 版ランタイムを同梱する。ucrtbased.dll は Windows SDK にある。
func bundleDebugCRT(dataDir string, problems []importProblem) (int, error) {
	dirs := vcRedistDirs(true)
	if sdk, _ := filepath.Glob(`C:\Program Files (x86)\Windows Kits\10\bin\*\x64\ucrt`); len(sdk) > 0 {
		sort.Sort(sort.Reverse(sort.StringSlice(sdk)))
		dirs = append(dirs, sdk...)
	}
	var names []string
	done := map[string]bool{}
	for _, p := range problems {
		low := strings.ToLower(p.DLL)
		if done[low] {
			continue
		}
		if err := copyFromDirs(low, dirs, dataDir); err != nil {
			return 0, fmt.Errorf("dist --debug: %w", err)
		}
		done[low] = true
		names = append(names, low)
	}
	if len(names) > 0 {
		fmt.Printf("dist --debug: 再頒布できない Debug 版ランタイムを同梱した (%s)\n", strings.Join(names, ", "))
	}
	return len(names), nil
}

// checkStandaloneExe は data/ の外に単独で置く exe (ランチャ) が、素の Windows にある DLL だけを読むか確かめる。
func checkStandaloneExe(path string) error {
	libs, err := readPEImports(path)
	if err != nil {
		return fmt.Errorf("dist: %s の import を読めない: %w", filepath.Base(path), err)
	}
	var bad []string
	for _, lib := range libs {
		low := strings.ToLower(lib)
		if vcRuntimeDLLs[low] || isDebugCRT(low) || mustBundleDLLs[low] || !systemDLLInWindows(lib) {
			bad = append(bad, lib)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("dist: %s は data/ の外に置くのに %s を読む (静的 CRT でビルドされていない)",
			filepath.Base(path), strings.Join(bad, ", "))
	}
	return nil
}

func formatImportProblems(ps []importProblem) string {
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "  data/%s → %s (%s)\n", p.Importer, p.DLL, p.Reason)
	}
	return strings.TrimRight(b.String(), "\n")
}
