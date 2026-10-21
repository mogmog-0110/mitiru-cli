package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindVcvars64UsesTheEnvironmentOverride(t *testing.T) {
	bat := filepath.Join(t.TempDir(), "vcvars64.bat")
	if err := os.WriteFile(bat, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MITIRU_VCVARS", bat)
	got, err := FindVcvars64()
	if err != nil || got != bat {
		t.Fatalf("FindVcvars64() = %q, %v; want %q", got, err, bat)
	}
}

func TestFindVcvars64RejectsAMissingOverride(t *testing.T) {
	t.Setenv("MITIRU_VCVARS", filepath.Join(t.TempDir(), "none", "vcvars64.bat"))
	if _, err := FindVcvars64(); err == nil {
		t.Fatal("存在しない MITIRU_VCVARS をエラーにしなかった")
	}
}
