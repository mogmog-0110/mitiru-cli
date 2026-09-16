package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLearnInspectPage(t *testing.T) {
	cases := []struct {
		name  string
		page  string
		learn bool
		want  string
	}{
		{"learn なし、page なし → 窓なし", "", false, ""},
		{"learn あり、page なし → scene (game memory タブ)", "", true, "scene?tab=memory"},
		{"learn あり、page 明示指定は上書きしない", "perf", true, "perf"},
		{"learn なし、page 明示指定はそのまま", "rewind", false, "rewind"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := learnInspectPage(c.page, c.learn)
			if got != c.want {
				t.Fatalf("learnInspectPage(%q, %v) = %q, want %q", c.page, c.learn, got, c.want)
			}
		})
	}
}

func TestWarnIfNoAutoReflect(t *testing.T) {
	t.Run("src/ に MITIRU_REFLECT が無ければ何か出力する状況を検出できる", func(t *testing.T) {
		root := t.TempDir()
		srcDir := filepath.Join(root, "src")
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			t.Fatalf("mkdir src: %v", err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, "main.cpp"), []byte("int main() {}\n"), 0o644); err != nil {
			t.Fatalf("write main.cpp: %v", err)
		}
		// warnIfNoAutoReflect は標準出力に書くだけで戻り値は無いため、panic しないことのみ確認する
		// (実際のメッセージ内容は目視/統合テストの対象。ここでは検出ロジックの分岐が
		// ファイル無しで落ちないことを保証する)。
		warnIfNoAutoReflect(root)
	})

	t.Run("src/ が無いプロジェクトでも落ちない", func(t *testing.T) {
		root := t.TempDir()
		warnIfNoAutoReflect(root)
	})

	t.Run("MITIRU_REFLECT_AUTO がある場合も落ちない", func(t *testing.T) {
		root := t.TempDir()
		srcDir := filepath.Join(root, "src")
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			t.Fatalf("mkdir src: %v", err)
		}
		content := "struct MyGame {};\nMITIRU_REFLECT_AUTO(MyGame);\n"
		if err := os.WriteFile(filepath.Join(srcDir, "main.cpp"), []byte(content), 0o644); err != nil {
			t.Fatalf("write main.cpp: %v", err)
		}
		warnIfNoAutoReflect(root)
	})
}
