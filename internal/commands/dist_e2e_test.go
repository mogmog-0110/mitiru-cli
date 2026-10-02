//go:build windows

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDistE2EAction3D は action3d の新しい project を作って mitiru dist --check を走らせ、配布物から
// 1 つずつ消したときに欠けが知らされるかを確かめる。engine を Release で丸ごとビルドするので遅い
// (初回は 10 分前後)。MITIRU_E2E_ENGINE_ROOT に engine の checkout を置いたときだけ走る。
// MITIRU_E2E_WORK を置くとそこで作業し、2 回目からビルドが差分で済む。
func TestDistE2EAction3D(t *testing.T) {
	engine := os.Getenv("MITIRU_E2E_ENGINE_ROOT")
	if engine == "" {
		t.Skip("MITIRU_E2E_ENGINE_ROOT が無い (engine をビルドする遅い e2e)")
	}
	work := os.Getenv("MITIRU_E2E_WORK")
	if work == "" {
		work = t.TempDir()
	}
	cli := filepath.Join(work, "mitiru.exe")
	runIn(t, filepath.Join("..", ".."), nil, "go", "build", "-o", cli, "./cmd/mitiru")
	project := filepath.Join(work, "e2e3d")
	if _, err := os.Stat(project); os.IsNotExist(err) {
		runIn(t, work, nil, cli, "new", "e2e3d", "-t", "action3d")
	}
	out := runIn(t, project, []string{"MITIRU_ENGINE_ROOT=" + engine}, cli, "dist", "--check")
	if !strings.Contains(out, "dist --check: OK") {
		t.Fatalf("dist --check did not pass:\n%s", out)
	}

	bundle := filepath.Join(project, "dist", "e2e3d")
	data := filepath.Join(bundle, "data")
	for _, rel := range []string{"e2e3d.exe", "data/vcruntime140.dll", "data/e2e3d/e2e3d.dll",
		"data/e2e3d/assets/level.obj", "data/e2e3d/assets/level.navmesh"} {
		if _, err := os.Stat(filepath.Join(bundle, filepath.FromSlash(rel))); err != nil {
			t.Errorf("bundle lacks %s: %v", rel, err)
		}
	}
	for _, rel := range []string{"data/compile_commands.json", "data/mitiru_build.json", "data/e2e3d_navbake_Release.stamp"} {
		if _, err := os.Stat(filepath.Join(bundle, filepath.FromSlash(rel))); err == nil {
			t.Errorf("bundle ships the dev-only file %s", rel)
		}
	}
	rep, err := checkDistImports(data, readPEImports, systemDLLInWindows)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.DebugCRT)+len(rep.Missing)+len(rep.MissingVC) != 0 {
		t.Errorf("a Release bundle must resolve every import from itself or Windows: %+v", rep)
	}

	// 遊ぶ側で欠けたとき: アセットとゲームの DLL は host のログで、ランタイムの DLL は依存の検査で分かる
	for _, rel := range []string{"data/e2e3d/assets/level.obj", "data/e2e3d/e2e3d.dll"} {
		broken := t.TempDir()
		if err := copyTree(bundle, broken); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(broken, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
		res, err := runDistCheck(broken, filepath.Join(t.TempDir(), "shot.png"))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Problems) == 0 {
			t.Errorf("removing %s went unnoticed by dist --check", rel)
		}
	}
	broken := t.TempDir()
	if err := copyTree(data, broken); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(broken, "vcruntime140.dll")); err != nil {
		t.Fatal(err)
	}
	if rep, _ := checkDistImports(broken, readPEImports, systemDLLInWindows); len(rep.MissingVC) == 0 {
		t.Error("removing vcruntime140.dll went unnoticed by the import check")
	}
}

func runIn(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}
