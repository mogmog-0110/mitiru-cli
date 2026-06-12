package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExtractBuildErrorsPicksMsvcErrorLinesWithContext(t *testing.T) {
	output := strings.Join([]string{
		"[1/3] Building CXX object game.dir/main.cpp.obj",
		"FAILED: game.dir/main.cpp.obj",
		`E:\proj\src\main.cpp(12): error C2065: 'foo': undeclared identifier`,
		"  note: see declaration of 'bar'",
		"[2/3] some unrelated progress line",
		"ninja: build stopped: subcommand failed.",
	}, "\r\n")

	got := ExtractBuildErrors(output, 20)

	if len(got) != 3 {
		t.Fatalf("expected 3 lines (error + 1 before/after), got %d: %v", len(got), got)
	}
	if got[0] != "FAILED: game.dir/main.cpp.obj" {
		t.Errorf("expected preceding context line first, got %q", got[0])
	}
	if !strings.Contains(got[1], "error C2065") {
		t.Errorf("expected error line second, got %q", got[1])
	}
	if !strings.Contains(got[2], "note:") {
		t.Errorf("expected following context line third, got %q", got[2])
	}
}

func TestExtractBuildErrorsRecognizesFatalAndLinkErrors(t *testing.T) {
	output := strings.Join([]string{
		`main.cpp(1): fatal error C1083: Cannot open include file: 'mitiru/x.hpp'`,
		"unrelated",
		"unrelated 2",
		"unrelated 3",
		`main.obj : error LNK2019: unresolved external symbol foo`,
	}, "\n")

	got := ExtractBuildErrors(output, 20)

	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "fatal error C1083") {
		t.Errorf("fatal error line missing: %v", got)
	}
	if !strings.Contains(joined, "error LNK2019") {
		t.Errorf("LNK error line missing: %v", got)
	}
	if strings.Contains(joined, "unrelated 2") {
		t.Errorf("non-context line should be excluded: %v", got)
	}
}

func TestExtractBuildErrorsCapsLineCount(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("x.cpp(1): error C2065: 'a': undeclared identifier\n")
	}

	got := ExtractBuildErrors(b.String(), 20)

	if len(got) != 20 {
		t.Fatalf("expected cap at 20 lines, got %d", len(got))
	}
}

func TestExtractBuildErrorsFallsBackToTailWhenNoErrorPattern(t *testing.T) {
	output := "configure step exploded\nsomething else went wrong\n"

	got := ExtractBuildErrors(output, 20)

	if len(got) != 2 {
		t.Fatalf("expected 2 tail lines, got %d: %v", len(got), got)
	}
	if got[1] != "something else went wrong" {
		t.Errorf("tail order wrong: %v", got)
	}
}

func TestWriteAndClearBuildErrorFileRoundTrip(t *testing.T) {
	root := t.TempDir()

	if err := WriteBuildErrorFile(root, "x.cpp(1): error C2065: boom\n"); err != nil {
		t.Fatalf("WriteBuildErrorFile: %v", err)
	}
	path := BuildErrorFilePath(root)
	if path != filepath.Join(root, "build", BuildErrorFileName) {
		t.Fatalf("unexpected error file path: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("error file not written: %v", err)
	}
	if !strings.Contains(string(data), "error C2065") {
		t.Errorf("error file content wrong: %q", string(data))
	}

	ClearBuildErrorFile(root)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("error file should be removed, stat err = %v", err)
	}

	// 二重 Clear は no-op
	ClearBuildErrorFile(root)
}

func TestWriteBuildErrorFileSanitizesInvalidUTF8(t *testing.T) {
	root := t.TempDir()
	// CP932 の「エ」(0x83 0x47) を含む擬似 MSVC 出力 — invalid UTF-8 bytes
	raw := "x.cpp(1): error C2065: \x83\x47 boom\n"

	if err := WriteBuildErrorFile(root, raw); err != nil {
		t.Fatalf("WriteBuildErrorFile: %v", err)
	}
	data, err := os.ReadFile(BuildErrorFilePath(root))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !utf8.ValidString(string(data)) {
		t.Errorf("file is not valid UTF-8: %q", string(data))
	}
}
