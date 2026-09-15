package build

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// includeTraceLineRe は MSVC /showIncludes が吐く 1 行 ("Note: including file:   <path>")
// を検出する。CMake は起動時の try_compile でこの prefix 文字列をローカライズ検出して
// build.ninja の msvc_deps_prefix に焼くが、検出に失敗すると ninja の deps=msvc
// フィルタが効かず、全 include 行が生で標準出力に流れる。header-only なこの
// エンジンでは合計 13 万行規模になる。
//
// prefix 文字列は cl の locale 設定に依存して変わりうるため、prefix そのものでは
// 一致させず、「コロンの後に絶対パスが続いて行末まで続く」という構造で判定する。
// <atomic> や <span> のような拡張子無し標準ヘッダも多いため拡張子は問わない。
var includeTraceLineRe = regexp.MustCompile(`[:：]\s+[A-Za-z]:[\\/][^\r\n]*$`)

// ninjaProgressLineRe は ninja の進捗行 ("[123/456] Building CXX object ...") を
// 検出する。
var ninjaProgressLineRe = regexp.MustCompile(`^\[(\d+)/(\d+)\]\s+(.*)$`)

// buildProgressFilter は cmake --build (ninja) の生出力を、
//   - /showIncludes の生ログ行を落とし、
//   - ninja の [N/M] 進捗行を "engine N 件 / user M 件" の 1 行更新表示に畳む
//
// io.Writer。対象外の行 (warning / error / cmake 診断) はそのまま素通しする。
// 失敗時の原因追跡 (ExtractBuildErrors) に必要な情報を落とさないための措置。
//
// target が空、または該当なしなら全て "engine" 側にカウントする (standalone
// ビルド等、user ターゲットが無い構成)。
type buildProgressFilter struct {
	underlying io.Writer
	// userDirMarker は progress 行の rest 部分に含まれれば "user" 側とみなす
	// 目印 (通常は sanitiseTargetName されたターゲット名の .dir)。
	userDirMarker string

	buf            bytes.Buffer
	engineDone     int
	userDone       int
	onProgressLine bool // 直前の書き込みが \r 上書き行だったか
	err            error
}

func newBuildProgressFilter(underlying io.Writer, targetName string) *buildProgressFilter {
	marker := ""
	if targetName != "" {
		marker = targetName + ".dir"
	}
	return &buildProgressFilter{underlying: underlying, userDirMarker: marker}
}

func (f *buildProgressFilter) Write(p []byte) (int, error) {
	n := len(p)
	f.buf.Write(p)
	for {
		data := f.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := string(data[:idx])
		f.buf.Next(idx + 1)
		f.handleLine(strings.TrimRight(line, "\r"))
	}
	if f.err != nil {
		return 0, f.err
	}
	return n, nil
}

// Finish は残った未改行のバッファを吐き出し、進捗行の上書き表示中だったなら
// 改行して締める。cmake --build 完了後に必ず呼ぶこと。
func (f *buildProgressFilter) Finish() error {
	if f.buf.Len() > 0 {
		f.handleLine(strings.TrimRight(f.buf.String(), "\r\n"))
		f.buf.Reset()
	}
	if f.onProgressLine {
		f.writeRaw("\n")
		f.onProgressLine = false
	}
	return f.err
}

func (f *buildProgressFilter) handleLine(line string) {
	if line == "" {
		return
	}
	if includeTraceLineRe.MatchString(line) {
		return
	}
	if m := ninjaProgressLineRe.FindStringSubmatch(line); m != nil {
		cur, total, rest := m[1], m[2], m[3]
		if f.userDirMarker != "" && strings.Contains(rest, f.userDirMarker) {
			f.userDone++
		} else {
			f.engineDone++
		}
		status := fmt.Sprintf("\r[%s/%s] engine:%d user:%d", cur, total, f.engineDone, f.userDone)
		f.writeRaw(status)
		f.onProgressLine = true
		return
	}
	// 通常の診断行 (warning / error / cmake message)。進捗行の上に重ねて
	// 出さないよう、直前が進捗行なら改行してから出す。
	if f.onProgressLine {
		f.writeRaw("\n")
		f.onProgressLine = false
	}
	f.writeRaw(line)
	f.writeRaw("\n")
}

func (f *buildProgressFilter) writeRaw(s string) {
	if f.err != nil {
		return
	}
	if _, err := io.WriteString(f.underlying, s); err != nil {
		f.err = err
	}
}
