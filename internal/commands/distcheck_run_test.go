package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDistCheckHostArgsPlaysTheScriptForTheGivenFrames(t *testing.T) {
	got := strings.Join(distCheckHostArgs(`game/game.dll --game-name g`,
		distCheckRun{Frames: 600, Script: `C:\play.txt`}, `C:\cap`), " ")
	for _, want := range []string{
		"game/game.dll --game-name g --headless-3d",
		"--max-frames 600",
		`--capture-every 599`,
		`--input-script C:\play.txt`,
		"--backend dx12",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q lack %q", got, want)
		}
	}
	idle := strings.Join(distCheckHostArgs("g.dll --backend dx11", distCheckRun{Frames: 90}, "c"), " ")
	if strings.Contains(idle, "--input-script") || strings.Contains(idle, "--backend dx12") {
		t.Errorf("idle check with its own backend got %q", idle)
	}
}

func TestResolveDistCheckRunRejectsAMissingScriptBeforeBuilding(t *testing.T) {
	defer func(c bool, s string, f int) { distCheck, distCheckScript, distCheckFrames = c, s, f }(
		distCheck, distCheckScript, distCheckFrames)

	distCheck, distCheckFrames = true, 300
	distCheckScript = filepath.Join(t.TempDir(), "nope.txt")
	if _, err := resolveDistCheckRun(); err == nil {
		t.Fatal("a missing --check-script must fail before the Release build")
	}

	distCheckScript = filepath.Join(t.TempDir(), "play.txt")
	if err := os.WriteFile(distCheckScript, []byte("10 W down\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run, err := resolveDistCheckRun()
	if err != nil || run.Script != distCheckScript || run.Frames != 300 {
		t.Fatalf("run = %+v, err = %v", run, err)
	}

	distCheckFrames = 1
	if _, err := resolveDistCheckRun(); err == nil {
		t.Error("--check-frames 1 cannot capture a frame after the first")
	}
}

func TestDistColdBuildNoticeNamesTheConfig(t *testing.T) {
	if !strings.Contains(distColdBuildNotice(false), "Release") || !strings.Contains(distColdBuildNotice(true), "Debug") {
		t.Error("the notice must say which build is about to run")
	}
}
