package build

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configureWith(t *testing.T, projectRoot, engineRoot string, mod func(*Options)) (stdout string, err error) {
	t.Helper()
	var out bytes.Buffer
	opts := Options{
		ProjectRoot: projectRoot,
		ProjectName: "game",
		EngineRoot:  engineRoot,
		Config:      "Debug",
		Stdout:      &out,
	}
	mod(&opts)
	_, _, err = Configure(opts)
	return out.String(), err
}

func newEngineWithHost(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	host := filepath.Join(root, "apps", "mitiru_host", "main.cpp")
	if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host, []byte("// host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func generatedText(t *testing.T, projectRoot string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(projectRoot, "build", "cmake", "CMakeLists.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 環境変数だけが engine を別の場所へ切り替えるなら、生成した CMakeLists.txt を書き換える前に止まる。
func TestConfigure_EnvOnlyEngineSwitchStopsBeforeRewriting(t *testing.T) {
	projectRoot, engineA := fakeProject(t)
	engineB := newEngineWithHost(t)
	if _, err := configureWith(t, projectRoot, engineA, func(o *Options) {}); err != nil {
		t.Fatalf("first configure: %v", err)
	}
	before := generatedText(t, projectRoot)

	_, err := configureWith(t, projectRoot, engineB, func(o *Options) { o.EngineFromEnv = true })
	var sw EngineSwitchError
	if !errors.As(err, &sw) {
		t.Fatalf("want EngineSwitchError, got %v", err)
	}
	if !strings.Contains(err.Error(), "--engine-root") || !strings.Contains(err.Error(), "MITIRU_ENGINE_ROOT") {
		t.Errorf("the message must say how to proceed: %q", err.Error())
	}
	if generatedText(t, projectRoot) != before {
		t.Error("the generated CMakeLists.txt was rewritten even though the switch was refused")
	}
}

// --engine-root で明示された切り替えは通り、全体を作り直すと知らせる。
func TestConfigure_ExplicitEngineSwitchGoesThroughWithANote(t *testing.T) {
	projectRoot, engineA := fakeProject(t)
	engineB := newEngineWithHost(t)
	if _, err := configureWith(t, projectRoot, engineA, func(o *Options) {}); err != nil {
		t.Fatalf("first configure: %v", err)
	}
	out, err := configureWith(t, projectRoot, engineB, func(o *Options) {
		o.EngineFromEnv, o.EngineSwitchExplicit = true, true
	})
	if err != nil {
		t.Fatalf("explicit switch must pass: %v", err)
	}
	if !strings.Contains(out, "作り直します") {
		t.Errorf("the rebuild must be announced: %q", out)
	}
	if !strings.Contains(generatedText(t, projectRoot), filepath.ToSlash(engineB)) {
		t.Error("the new engine root must be written")
	}
}

// 環境変数が関わらない切り替え (mitiru.toml の pin を上げたとき) は、今までどおり知らせるだけ。
func TestConfigure_PinChangeIsNotBlocked(t *testing.T) {
	projectRoot, engineA := fakeProject(t)
	engineB := newEngineWithHost(t)
	if _, err := configureWith(t, projectRoot, engineA, func(o *Options) {}); err != nil {
		t.Fatalf("first configure: %v", err)
	}
	if _, err := configureWith(t, projectRoot, engineB, func(o *Options) {}); err != nil {
		t.Fatalf("a pin bump must not be blocked: %v", err)
	}
}

// 環境変数で決めた同じ場所のまま使い続けるのは止めない (切り替えが無いので静か)。
func TestConfigure_SameEnvEngineIsQuiet(t *testing.T) {
	projectRoot, engineA := fakeProject(t)
	if _, err := configureWith(t, projectRoot, engineA, func(o *Options) { o.EngineFromEnv = true }); err != nil {
		t.Fatalf("first configure: %v", err)
	}
	out, err := configureWith(t, projectRoot, engineA, func(o *Options) { o.EngineFromEnv = true })
	if err != nil || out != "" {
		t.Fatalf("same engine must pass quietly: out=%q err=%v", out, err)
	}
}
