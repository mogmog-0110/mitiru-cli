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
	if err := f.Finish(); err != nil {
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
	if err := f.Finish(); err != nil {
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
	if err := f.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if f.engineDone != 1 {
		t.Errorf("expected 1 engine-bucket line (mitiru_host), got %d", f.engineDone)
	}
	if f.userDone != 2 {
		t.Errorf("expected 2 user-bucket lines (probe target), got %d", f.userDone)
	}
}
