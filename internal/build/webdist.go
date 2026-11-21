package build

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// emscriptenToolchainRel は emsdk の下の Emscripten のツールチェーンファイル。
const emscriptenToolchainRel = "upstream/emscripten/cmake/Modules/Platform/Emscripten.cmake"

// webGameModule は engine の中の、ブラウザ向けのゲームを組む CMake の関数 (mitiru_add_web_game) のファイル。
const webGameModule = "cmake/MitiruWebGame.cmake"

// WebOptions は mitiru dist --web が生成する CMake の入力。
type WebOptions struct {
	ProjectName string
	EngineRoot  string
	MainCpp     string // ゲームの main.cpp の絶対パス
	AssetsDir   string // 無ければ空。あれば wasm の仮想ファイルシステムの /assets に載る
	Title       string
	Width       int
	Height      int
	SingleFile  bool
	Sources     []string // [build] sources を広げた絶対パス (CMake の書き方)。main.cpp のほかに wasm へ足す cpp
}

// WebTargetName は web 用の CMake の target 名。DLL の target と同じ規則で作る。
func WebTargetName(projectName string) string { return TargetName(projectName) }

// EmsdkCandidates は emsdk を探す場所を、探す順に並べる。EMSDK 環境変数、E:/emsdk、C:/emsdk、~/emsdk。
func EmsdkCandidates(getenv func(string) string, home string) []string {
	var candidates []string
	if v := strings.TrimSpace(getenv("EMSDK")); v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates, "E:/emsdk", "C:/emsdk")
	if home != "" {
		candidates = append(candidates, filepath.ToSlash(filepath.Join(home, "emsdk")))
	}
	return candidates
}

// FindEmsdk は candidates のうち、ツールチェーンファイルと環境を整える emsdk_env.bat の両方がある最初の場所を返す。
// 無ければ探した場所を並べた error を返す。
func FindEmsdk(candidates []string) (string, error) {
	for _, c := range candidates {
		dir := filepath.FromSlash(c)
		_, tcErr := os.Stat(filepath.Join(dir, filepath.FromSlash(emscriptenToolchainRel)))
		_, envErr := os.Stat(filepath.Join(dir, "emsdk_env.bat"))
		if tcErr == nil && envErr == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("Emscripten の SDK (emsdk) が見つかりません。探した場所: %s", strings.Join(candidates, "、"))
}

// EmsdkToolchainFile は emsdk の下のツールチェーンファイルの場所 (CMake の書き方)。
func EmsdkToolchainFile(emsdk string) string {
	return toCMakePath(filepath.Join(emsdk, filepath.FromSlash(emscriptenToolchainRel)))
}

// CheckWebEngine は engine が、ブラウザ向けの配布に要るファイルをそろえているかを調べる。
func CheckWebEngine(engineRoot string) error {
	for _, rel := range []string{webGameModule, "web/web_main.cpp", "web/dist_shell.html"} {
		if _, err := os.Stat(filepath.Join(engineRoot, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("このエンジン (%s) には %s がありません。mitiru dist --web は 0.54.0 以降のエンジンが要ります。"+
				"mitiru.toml の engine を上げる (mitiru update) か、--engine-root で新しいエンジンの場所を指してください。",
				engineRoot, rel)
		}
	}
	return nil
}

const webCMakeTemplate = `# mitiru-cli が mitiru dist --web のために生成した CMakeLists.txt。手で直さないでください。
cmake_minimum_required(VERSION 3.21)
project({{.Target}}_web LANGUAGES C CXX)

set(MITIRU_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(MITIRU_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(MITIRU_ENGINE_ROOT "{{.EngineRoot}}")
add_subdirectory("${MITIRU_ENGINE_ROOT}" mitiru-engine)
include("${MITIRU_ENGINE_ROOT}/cmake/MitiruWebGame.cmake")

mitiru_add_web_game({{.Target}}
    MAIN "{{.MainCpp}}"
{{- if .AssetsDir}}
    ASSETS "{{.AssetsDir}}"
{{- end}}
    TITLE {{.Title}}
{{- if gt .Width 0}}
    WIDTH {{.Width}}
{{- end}}
{{- if gt .Height 0}}
    HEIGHT {{.Height}}
{{- end}}
{{- if .SingleFile}}
    SINGLE_FILE
{{- end}})
target_include_directories({{.Target}} PRIVATE "{{.MainDir}}")
{{- if .Sources}}
target_sources({{.Target}} PRIVATE
{{- range .Sources}}
    "{{.}}"
{{- end}})
{{- end}}
`

type webCMakeData struct {
	Target     string
	EngineRoot string
	MainCpp    string
	MainDir    string
	AssetsDir  string
	Title      string // CMake の引用符つき
	Width      int
	Height     int
	SingleFile bool
	Sources    []string
}

// RenderWebCMake は ADR 0163 の「決定」3 の CMake を作る。SHARED のゲーム DLL は作らず、
// engine を add_subdirectory して mitiru_add_web_game の 1 つの target だけを定義する。
func RenderWebCMake(o WebOptions) (string, error) {
	title := o.Title
	if strings.TrimSpace(title) == "" {
		title = o.ProjectName
	}
	data := webCMakeData{
		Target:     WebTargetName(o.ProjectName),
		EngineRoot: toCMakePath(o.EngineRoot),
		MainCpp:    toCMakePath(o.MainCpp),
		MainDir:    toCMakePath(filepath.Dir(o.MainCpp)),
		Title:      cmakeQuote(title),
		Width:      o.Width,
		Height:     o.Height,
		SingleFile: o.SingleFile,
		Sources:    o.Sources,
	}
	if o.AssetsDir != "" {
		data.AssetsDir = toCMakePath(o.AssetsDir)
	}
	tmpl, err := template.New("web").Option("missingkey=error").Parse(webCMakeTemplate)
	if err != nil {
		return "", fmt.Errorf("parse web cmake template: %w", err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render web cmake template: %w", err)
	}
	return out.String(), nil
}

// WebDirs は mitiru dist --web の生成物の標準の置き場:
//
//	<projectRoot>/build/web/cmake/   生成した CMakeLists.txt
//	<projectRoot>/build/web/out/     cmake -B の出力。ゲームは out/<target>/ にできる
func WebDirs(projectRoot string) (cmakeSrcDir, cmakeOutDir string) {
	base := filepath.Join(projectRoot, "build", "web")
	return filepath.Join(base, "cmake"), filepath.Join(base, "out")
}

// CountNetworkURLs は dir の下の text ファイルにある http:// と https:// の数を返す。
// ブラウザ向けの配布物は CDN に頼らないので、0 であることを確かめる材料にする。
func CountNetworkURLs(dir string) (int, error) {
	n := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".html", ".js", ".css", ".json":
		default:
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		n += bytes.Count(body, []byte("http://")) + bytes.Count(body, []byte("https://"))
		return nil
	})
	return n, err
}
