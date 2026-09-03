package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ManifestFilename)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_StandaloneNeedsNoEnginePin(t *testing.T) {
	cfg, err := Load(writeManifest(t, `
[project]
name = "desktop_world"

[build]
kind = "standalone"
source = "src"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Standalone() {
		t.Fatal("expected a standalone project")
	}
	if cfg.Build.Source != "src" {
		t.Fatalf("build.source = %q", cfg.Build.Source)
	}
	if cfg.Build.Target != "desktop_world" {
		t.Fatalf("build.target should default to project.name, got %q", cfg.Build.Target)
	}
}

func TestLoad_HostStillNeedsEnginePin(t *testing.T) {
	_, err := Load(writeManifest(t, `
[project]
name = "my-game"
`))
	if err == nil {
		t.Fatal("a host project without project.engine must be rejected")
	}
}

func TestLoad_DefaultsToHostKind(t *testing.T) {
	cfg, err := Load(writeManifest(t, `
[project]
name = "my-game"
engine = "0.32.0"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Build.Kind != BuildKindHost || cfg.Standalone() {
		t.Fatalf("build.kind should default to host, got %q", cfg.Build.Kind)
	}
	if cfg.Build.Source != "." {
		t.Fatalf("build.source should default to \".\", got %q", cfg.Build.Source)
	}
}

func TestLoad_RejectsUnknownKindAndAbsoluteSource(t *testing.T) {
	if _, err := Load(writeManifest(t, "[project]\nname = \"x\"\nengine = \"0.32.0\"\n[build]\nkind = \"dll\"\n")); err == nil {
		t.Fatal("an unknown build.kind must be rejected")
	}
	if _, err := Load(writeManifest(t, "[project]\nname = \"x\"\n[build]\nkind = \"standalone\"\nsource = \"C:/elsewhere\"\n")); err == nil {
		t.Fatal("an absolute build.source must be rejected")
	}
}
