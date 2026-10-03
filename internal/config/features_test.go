package config

import (
	"strings"
	"testing"
)

const hostHeader = `
[project]
name = "game"
engine = "0.35.0"
`

func TestLoad_EngineFeaturesAndNav(t *testing.T) {
	cfg, err := Load(writeManifest(t, hostHeader+`
[engine]
features = ["jolt", "nav", "NAV"]

[nav]
source = "assets/levels/arena.glb"
args = ["--radius", "0.4"]
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := ResolveFeatures(cfg.Engine.Features)
	if err != nil {
		t.Fatalf("ResolveFeatures: %v", err)
	}
	// 重複はまとめ、並びは表の順 (nav, navbake, jolt)
	if len(got) != 2 || got[0].Name != "nav" || got[1].Name != "jolt" {
		t.Fatalf("features = %+v", got)
	}
	if got[1].Link {
		t.Error("jolt is already linked into mitiru; it should only be checked")
	}
	if p := cfg.NavMeshPath(); p != "assets/levels/arena.navmesh" {
		t.Errorf("NavMeshPath = %q", p)
	}
	if len(cfg.Nav.Args) != 2 {
		t.Errorf("nav.args = %v", cfg.Nav.Args)
	}
}

func TestLoad_NoEngineSectionIsFine(t *testing.T) {
	cfg, err := Load(writeManifest(t, hostHeader))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Engine.Features) != 0 || cfg.NavMeshPath() != "" {
		t.Fatalf("unexpected engine/nav defaults: %+v %+v", cfg.Engine, cfg.Nav)
	}
}

func TestLoad_EngineErrors(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"unknown feature lists the known ones", `
[engine]
features = ["rocket"]`, []string{`unknown feature "rocket"`, "nav, navbake, jolt"}},
		{"common mistake gets a hint", `
[engine]
features = ["crowd"]`, []string{`"crowd"`, `NavCrowd is part of "nav"`}},
		{"fbx is part of the engine", `
[engine]
features = ["fbx"]`, []string{"FBX import is always part of the engine"}},
		{"nav source needs a nav feature", `
[nav]
source = "assets/level.obj"`, []string{`add "nav" to [engine] features`}},
		{"nav source must be a mesh", `
[engine]
features = ["nav"]
[nav]
source = "assets/level.json"`, []string{"must be a level mesh", ".glb / .gltf / .obj"}},
		{"nav source stays in the project", `
[engine]
features = ["nav"]
[nav]
source = "../shared/level.obj"`, []string{"must stay inside the project"}},
		{"nav source is relative", `
[engine]
features = ["nav"]
[nav]
source = "C:/levels/level.obj"`, []string{"must be relative"}},
		{"nav args need a source", `
[engine]
features = ["nav"]
[nav]
args = ["--radius", "0.4"]`, []string{"nav.args needs nav.source"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(writeManifest(t, hostHeader+c.body))
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestLoad_StandaloneRejectsEngineFeatures(t *testing.T) {
	_, err := Load(writeManifest(t, `
[project]
name = "desktop_world"

[build]
kind = "standalone"

[engine]
features = ["nav"]
`))
	if err == nil || !strings.Contains(err.Error(), "only apply to host projects") {
		t.Fatalf("want a standalone error, got %v", err)
	}
}

func TestNavMeshPathFor(t *testing.T) {
	for in, want := range map[string]string{
		"assets/level.obj":         "assets/level.navmesh",
		`assets\maps\a.gltf`:       "assets/maps/a.navmesh",
		"./assets/../assets/b.glb": "assets/b.navmesh",
	} {
		if got := NavMeshPathFor(in); got != want {
			t.Errorf("NavMeshPathFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoad_LightingSourceStringOrList(t *testing.T) {
	cfg, err := Load(writeManifest(t, hostHeader+`
[lighting]
source = "assets/*.lighting.json"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Lighting.Source) != 1 || cfg.Lighting.Source[0] != "assets/*.lighting.json" {
		t.Errorf("lighting.source = %v", cfg.Lighting.Source)
	}

	cfg, err = Load(writeManifest(t, hostHeader+`
[lighting]
source = ["assets/a.lighting.json", 'assets\maps\*.lighting.json']
args = ["--rays", "512"]
`))
	if err != nil {
		t.Fatalf("Load list: %v", err)
	}
	if len(cfg.Lighting.Source) != 2 || len(cfg.Lighting.Args) != 2 {
		t.Errorf("lighting = %+v", cfg.Lighting)
	}
}

func TestLoad_LightingErrors(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"must be a lighting json", `
[lighting]
source = "assets/level.glb"`, "must name *.lighting.json"},
		{"stays in the project", `
[lighting]
source = "../shared/a.lighting.json"`, "must stay inside the project"},
		{"is relative", `
[lighting]
source = "C:/levels/a.lighting.json"`, "must be relative"},
		{"broken glob", `
[lighting]
source = "assets/[a.lighting.json"`, "broken glob"},
		{"args need a source", `
[lighting]
args = ["--rays", "512"]`, "lighting.args needs lighting.source"},
		{"strings only", `
[lighting]
source = 3`, "string or a list of strings"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(writeManifest(t, hostHeader+c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestLoad_StandaloneRejectsLighting(t *testing.T) {
	_, err := Load(writeManifest(t, `
[project]
name = "desktop_world"

[build]
kind = "standalone"

[lighting]
source = "assets/a.lighting.json"
`))
	if err == nil || !strings.Contains(err.Error(), "only applies to host projects") {
		t.Fatalf("want a standalone error, got %v", err)
	}
}

func TestLightingBinPathFor(t *testing.T) {
	for in, want := range map[string]string{
		"assets/stage.lighting.json":         "assets/stage.lighting.bin",
		`assets\maps\a.Lighting.JSON`:        "assets/maps/a.lighting.bin",
		"./assets/../assets/b.lighting.json": "assets/b.lighting.bin",
	} {
		if got := LightingBinPathFor(in); got != want {
			t.Errorf("LightingBinPathFor(%q) = %q, want %q", in, got, want)
		}
	}
}
