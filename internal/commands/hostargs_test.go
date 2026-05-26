package commands

import (
	"strings"
	"testing"

	"github.com/mogmog-0110/mitiru-cli/internal/config"
)

// joins host args with spaces for easy substring assertions.
func argsStr(pc *config.ProjectConfig) string {
	return strings.Join(hostArgsFromConfig(pc), " ")
}

func TestHostArgs_WindowAndFont(t *testing.T) {
	pc := &config.ProjectConfig{}
	pc.Window.Width, pc.Window.Height = 800, 600
	pc.Font.Atlas = "japanese"
	got := argsStr(pc)
	if !strings.Contains(got, "--size 800x600") {
		t.Errorf("missing --size: %q", got)
	}
	if !strings.Contains(got, "--font japanese") {
		t.Errorf("missing --font: %q", got)
	}
}

func TestHostArgs_FontNoneOmitted(t *testing.T) {
	pc := &config.ProjectConfig{}
	pc.Font.Atlas = "none"
	if strings.Contains(argsStr(pc), "--font") {
		t.Errorf("--font should be omitted for none: %q", argsStr(pc))
	}
}

func TestHostArgs_LofiDisabledEmitsNothing(t *testing.T) {
	pc := &config.ProjectConfig{}
	pc.Lofi.Enabled = false
	pc.Lofi.Width, pc.Lofi.Height = 640, 480 // enabled=false なら無視される
	if strings.Contains(argsStr(pc), "--lofi") {
		t.Errorf("lofi must be gated by enabled: %q", argsStr(pc))
	}
}

func TestHostArgs_LofiFull(t *testing.T) {
	d := 0.5
	pc := &config.ProjectConfig{}
	pc.Lofi.Enabled = true
	pc.Lofi.Width, pc.Lofi.Height = 640, 480
	pc.Lofi.Bits = "3,3,2"
	pc.Lofi.Dither = &d
	got := argsStr(pc)
	for _, want := range []string{"--lofi", "--lofi-size 640x480", "--lofi-bits 3,3,2", "--lofi-dither 0.5"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestHostArgs_LofiEnabledMinimal(t *testing.T) {
	pc := &config.ProjectConfig{}
	pc.Lofi.Enabled = true // width/height/bits/dither 未指定 → host 既定 (320x240 RGB565)
	got := argsStr(pc)
	if !strings.Contains(got, "--lofi") {
		t.Errorf("expected bare --lofi: %q", got)
	}
	if strings.Contains(got, "--lofi-size") || strings.Contains(got, "--lofi-bits") ||
		strings.Contains(got, "--lofi-dither") {
		t.Errorf("unspecified lofi fields must be omitted: %q", got)
	}
}

// dither=0 を明示した場合はディザ無効として 0 を渡す（未指定 nil と区別）。
func TestHostArgs_LofiDitherZeroExplicit(t *testing.T) {
	d := 0.0
	pc := &config.ProjectConfig{}
	pc.Lofi.Enabled = true
	pc.Lofi.Dither = &d
	if !strings.Contains(argsStr(pc), "--lofi-dither 0") {
		t.Errorf("explicit dither=0 must be passed: %q", argsStr(pc))
	}
}
