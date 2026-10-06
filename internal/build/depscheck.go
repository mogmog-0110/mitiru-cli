package build

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type objectDeps struct {
	Path  string
	Count int
}

// parseObjectDeps は `ninja -t deps` の出力から .obj ごとの header の依存の数を取り出す。
// 行は "path: #deps N, deps mtime ..." の形で、依存の path は次の行から字下げして続く。
// .rc.res など /showIncludes を使わない出力は、依存が空でも正しいので数えない。
func parseObjectDeps(out string) []objectDeps {
	var objs []objectDeps
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		i := strings.Index(line, ": #deps ")
		if i < 0 || !strings.HasSuffix(line[:i], ".obj") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(line[i+len(": #deps "):], "%d,", &n); err == nil {
			objs = append(objs, objectDeps{Path: line[:i], Count: n})
		}
	}
	return objs
}

// brokenDeps は今回作った object のうち、依存が記録されていないはずのものを返す。
// 本物のゲームの source は必ず header を include するので、空なら壊れている。
// engine には #if で中身が空になる source (Effekseer.Socket.cpp など) があり、
// 1 個だけ空なのは正常。半分以上が空なら、接頭辞の食い違いのように全体が壊れている。
func brokenDeps(compiled []objectDeps, gameDir string) []string {
	var empty []objectDeps
	for _, o := range compiled {
		if o.Count == 0 {
			empty = append(empty, o)
		}
	}
	wholesale := len(compiled) >= 3 && 2*len(empty) >= len(compiled)
	var bad []string
	for _, o := range empty {
		if wholesale || strings.HasPrefix(o.Path, gameDir) {
			bad = append(bad, o.Path)
		}
	}
	return bad
}

// verifyDepsRecorded は今回の build で作られたのに依存を記録しなかった object を探し、
// 見つけたらその object を消して error を返す。消すのは、残すと ninja がそれを最新と見て、
// header を直しても二度と組み直さないから。
func verifyDepsRecorded(vcvars, outDir string, since time.Time, opts Options) error {
	if os.Getenv("MITIRU_DRY_RUN") == "1" {
		return nil
	}
	listing := filepath.Join(outDir, "mitiru_deps_listing.txt")
	defer func() { _ = os.Remove(listing) }()
	script := vcvarsPrelude(vcvars) +
		"\"" + cachedMakeProgram(outDir) + "\" -C \"" + outDir + "\" -t deps > \"" + listing + "\" 2>NUL\r\n"
	quiet := opts
	quiet.Stdout = nil
	quiet.Stderr = nil
	if err := runBatchScript("mitiru_depscheck", script, quiet); err != nil {
		return nil // 確かめられないことで build 全体を落とさない
	}
	data, err := os.ReadFile(listing)
	if err != nil {
		return nil
	}
	var compiled []objectDeps
	for _, o := range parseObjectDeps(string(data)) {
		st, statErr := os.Stat(filepath.Join(outDir, filepath.FromSlash(o.Path)))
		if statErr == nil && !st.ModTime().Before(since) {
			compiled = append(compiled, o)
		}
	}
	bad := brokenDeps(compiled, "CMakeFiles/"+TargetName(opts.ProjectName)+".dir/")
	for _, obj := range bad {
		_ = os.Remove(filepath.Join(outDir, filepath.FromSlash(obj)))
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("header の依存が記録されなかった object が %d 個ありました (例: %s)。"+
		"このままだと header を直しても組み直されないので object を消しました。"+
		"sccache の古い cache が原因なら、sccache --stop-server と cache dir の削除か MITIRU_SCCACHE=off で組み直してください。",
		len(bad), bad[0])
}

// cachedMakeProgram は configure が選んだ ninja を返す。VS が複数あると PATH 上の ninja の版が違い、
// 別の版で `-t deps` を引くと .ninja_log を「古い」と捨てて全部を作り直させてしまう。
func cachedMakeProgram(outDir string) string {
	data, err := os.ReadFile(filepath.Join(outDir, "CMakeCache.txt"))
	if err != nil {
		return "ninja"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "CMAKE_MAKE_PROGRAM:FILEPATH="); ok {
			if p := strings.TrimSpace(rest); p != "" {
				return filepath.FromSlash(p)
			}
		}
	}
	return "ninja"
}
