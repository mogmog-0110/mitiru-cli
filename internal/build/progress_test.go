package build

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildProgressFilterDropsShowIncludesNoise(t *testing.T) {
	var out bytes.Buffer
	f := newBuildProgressFilter(&out, "probe")

	lines := []string{
		`Note: including file:   E:\user\MitiruEngine\include\mitiru\core\Screen.hpp`,
		`Note: including file:    E:\user\MitiruEngine\include\mitiru\module\Game.hpp`,
		`[1/2] Building CXX object CMakeFiles/mitiru_host.dir/main.cpp.obj`,
		`[2/2] Building CXX object CMakeFiles/probe.dir/src/main.cpp.obj`,
	}
	if _, err := f.Write([]byte(strings.Join(lines, "\r\n") + "\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Finish(true); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "including file") {
		t.Errorf("showIncludes noise leaked through: %q", got)
	}
	if got == "" {
		t.Fatal("expected some progress output, got nothing")
	}
	if strings.Count(got, "\n") > 1 {
		t.Errorf("expected pure-progress run to collapse to at most one trailing newline, got %d: %q",
			strings.Count(got, "\n"), got)
	}
}

func TestBuildProgressFilterKeepsRealErrorLines(t *testing.T) {
	var out bytes.Buffer
	f := newBuildProgressFilter(&out, "probe")

	lines := []string{
		`[1/2] Building CXX object CMakeFiles/probe.dir/src/main.cpp.obj`,
		`E:\proj\src\main.cpp(12): error C2065: 'foo': undeclared identifier`,
		`FAILED: CMakeFiles/probe.dir/src/main.cpp.obj`,
	}
	if _, err := f.Write([]byte(strings.Join(lines, "\r\n") + "\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Finish(true); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "error C2065") {
		t.Errorf("expected the real compiler error line to survive filtering, got %q", got)
	}
	if !strings.Contains(got, "FAILED: CMakeFiles/probe.dir/src/main.cpp.obj") {
		t.Errorf("expected the FAILED marker line to survive filtering, got %q", got)
	}
}

func TestBuildProgressFilterBucketsEngineVsUser(t *testing.T) {
	var out bytes.Buffer
	f := newBuildProgressFilter(&out, "probe")

	_, _ = f.Write([]byte("[1/3] Building CXX object CMakeFiles/mitiru_host.dir/main.cpp.obj\r\n"))
	_, _ = f.Write([]byte("[2/3] Building CXX object CMakeFiles/probe.dir/src/main.cpp.obj\r\n"))
	_, _ = f.Write([]byte("[3/3] Building CXX object CMakeFiles/probe.dir/src/other.cpp.obj\r\n"))
	if err := f.Finish(true); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if f.engineDone != 1 {
		t.Errorf("expected 1 engine-bucket line (mitiru_host), got %d", f.engineDone)
	}
	if f.userDone != 2 {
		t.Errorf("expected 2 user-bucket lines (probe target), got %d", f.userDone)
	}
}

func TestQuietBuildProgressFilterLeavesNothingOnSuccess(t *testing.T) {
	var out bytes.Buffer
	f := newQuietBuildProgressFilter(&out, "probe", `E:\game`)

	lines := []string{
		`[1/2] Building CXX object CMakeFiles/probe.dir/src/main.cpp.obj`,
		`E:\engine\include\mitiru\x.hpp(3): warning C4100: 'x': unreferenced formal parameter`,
		`ninja: no work to do.`,
		`[2/2] Linking CXX shared library probe\probe.dll`,
	}
	_, _ = f.Write([]byte(strings.Join(lines, "\r\n") + "\r\n"))
	if err := f.Finish(true); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "warning") || strings.Contains(got, "ninja:") || strings.Contains(got, "\n") {
		t.Errorf("quiet success should leave no lines, got %q", got)
	}
	// 書き先が端末でないので進捗行はそもそも書かれない。端末なら \r で消してある。
	if got != "" && !strings.HasSuffix(got, "\r") {
		t.Errorf("quiet success should leave nothing or an erased progress line, got %q", got)
	}
}

func TestQuietBuildProgressFilterShowsHeldLinesOnFailure(t *testing.T) {
	var out bytes.Buffer
	f := newQuietBuildProgressFilter(&out, "probe", `E:\game`)

	lines := []string{
		`[1/2] Building CXX object CMakeFiles/probe.dir/src/main.cpp.obj`,
		`E:\proj\src\main.cpp(12): error C2065: 'foo': undeclared identifier`,
		`FAILED: CMakeFiles/probe.dir/src/main.cpp.obj`,
	}
	_, _ = f.Write([]byte(strings.Join(lines, "\r\n") + "\r\n"))
	if err := f.Finish(false); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "error C2065") || !strings.Contains(got, "FAILED:") {
		t.Errorf("held error lines should appear on failure, got %q", got)
	}
}

// 利用者のコードを指す警告 (非推奨の角度の関数など) は、成功したビルドでも出す。
// エンジンや第三者のコードの警告は今までどおり出さない。
func TestQuietBuildProgressFilterShowsUserWarningsOnSuccess(t *testing.T) {
	var out bytes.Buffer
	f := newQuietBuildProgressFilter(&out, "probe", `E:/Tmp/Game`)

	user := `e:\tmp\game\src\main.cpp(157): warning C4996: 'mitiru::Screen::pushRotation': deprecated`
	lines := []string{
		`[1/3] Building CXX object CMakeFiles/probe.dir/src/main.cpp.obj`,
		user,
		`E:\tmp\gamekit\src\other.cpp(1): warning C4100: 'y': unreferenced formal parameter`,
		`E:\engine\include\mitiru\x.hpp(3): warning C4100: 'x': unreferenced formal parameter`,
		`E:\tmp\game\src\main.cpp(9): note: see declaration`,
		`[3/3] Linking CXX shared library probe\probe.dll`,
	}
	_, _ = f.Write([]byte(strings.Join(lines, "\r\n") + "\r\n"))
	if err := f.Finish(true); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, user+"\n") {
		t.Errorf("a warning in the project must be shown on success, got %q", got)
	}
	for _, hidden := range []string{"gamekit", `mitiru\x.hpp`, "note:"} {
		if strings.Contains(got, hidden) {
			t.Errorf("%q is outside the project's warnings and must stay hidden, got %q", hidden, got)
		}
	}
}
