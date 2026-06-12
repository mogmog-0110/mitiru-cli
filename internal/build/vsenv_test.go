package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVsEnvPath(t *testing.T) {
	out := "noise line\r\n" +
		vsEnvMarker + `C:\VS\bin;C:\Windows\system32` + "\r\n" +
		"trailing\r\n"
	p, ok := parseVsEnvPath(out)
	if !ok || p != `C:\VS\bin;C:\Windows\system32` {
		t.Errorf("parseVsEnvPath = (%q, %v)", p, ok)
	}

	if _, ok := parseVsEnvPath("no marker here\r\n"); ok {
		t.Error("parseVsEnvPath(no marker) = ok, want !ok")
	}
	if _, ok := parseVsEnvPath(vsEnvMarker + "\r\n"); ok {
		t.Error("parseVsEnvPath(empty value) = ok, want !ok")
	}
}

func TestPrependPath_ReplacesCaseInsensitive(t *testing.T) {
	env := []string{"FOO=bar", `Path=C:\old`, "BAZ=1"}
	got := PrependPath(env, `C:\vs`)

	want := `Path=C:\vs;C:\old`
	found := false
	for _, kv := range got {
		if kv == want {
			found = true
		}
	}
	if !found {
		t.Errorf("PrependPath = %v, want entry %q", got, want)
	}
	// 元 slice は不変 (immutability)。
	if env[1] != `Path=C:\old` {
		t.Errorf("input slice mutated: %v", env)
	}
}

func TestPrependPath_AddsWhenMissing(t *testing.T) {
	got := PrependPath([]string{"FOO=bar"}, `C:\vs`)
	if len(got) != 2 || got[1] != `PATH=C:\vs` {
		t.Errorf("PrependPath(no PATH) = %v", got)
	}
}

func TestFindInPathList(t *testing.T) {
	dir := t.TempDir()
	dll := filepath.Join(dir, "ucrtbased.dll")
	if err := os.WriteFile(dll, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	pathList := strings.Join([]string{`C:\definitely\missing`, "", dir}, ";")
	if !FindInPathList(pathList, "ucrtbased.dll") {
		t.Error("FindInPathList = false, want true")
	}
	if FindInPathList(pathList, "not_there.dll") {
		t.Error("FindInPathList(absent dll) = true, want false")
	}
}
