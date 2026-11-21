package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandGameSources(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"src/main.cpp", "src/a/A.cpp", "src/a/A.hpp", "src/b/B.cpp"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mainCpp := filepath.Join(root, "src", "main.cpp")

	got, err := ExpandGameSources(root, mainCpp, []string{"src/*.cpp", "src/*/*.cpp", "src/a/A.cpp"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.HasSuffix(got[0], "/src/a/A.cpp") || !strings.HasSuffix(got[1], "/src/b/B.cpp") {
		t.Fatalf("main.cpp を除き、重複なく名前順に並ぶはず: %v", got)
	}

	if _, err := ExpandGameSources(root, mainCpp, []string{"src/none/*.cpp"}); err == nil {
		t.Fatal("何にも当たらないパターンは error にする")
	}
}
