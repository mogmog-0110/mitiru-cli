package commands

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mogmog-0110/mitiru-cli/internal/console"
)

func writeFiles(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDistBakeListScansKinds(t *testing.T) {
	assets := t.TempDir()
	writeFiles(t, assets,
		"hero.fbx", "hero.fbx.glb", "pine.glb", "ship.gltf", "ship.bin", "chara.vrm",
		"level.obj", "level.obj.clod", "level.mtl", "baked.clod",
		"island.world.json", "island.world.json.m0.base.dds", "map/north.region.json",
		"stage.lighting.json", "stage.lighting.bin",
		"balance.json", "grass.png", "ui/main.rml", "ui/icon.glb")

	got, source, err := distBakeList(assets, "g/assets")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"clod:g/assets/baked.clod",
		"model:g/assets/chara.vrm",
		"model:g/assets/hero.fbx",
		"g/assets/island.world.json",
		"clod:g/assets/level.obj",
		"g/assets/map/north.region.json",
		"model:g/assets/pine.glb",
		"model:g/assets/ship.gltf",
		"g/assets/stage.lighting.bin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scan:\n got %q\nwant %q", got, want)
	}
	if !strings.Contains(source, "走査") {
		t.Errorf("source = %q, want the scan", source)
	}
}

func TestDistBakeListPrefersBakeTxt(t *testing.T) {
	assets := t.TempDir()
	writeFiles(t, assets, "pine.glb", "level.obj")
	txt := "# 世界のモデルとして焼く\nclod: assets/pine.glb\n\n  assets/island.world.json  # 区画\n"
	if err := os.WriteFile(filepath.Join(assets, "bake.txt"), []byte(txt), 0o644); err != nil {
		t.Fatal(err)
	}
	got, source, err := distBakeList(assets, "g/assets")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"clod: assets/pine.glb", "assets/island.world.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bake.txt:\n got %q\nwant %q", got, want)
	}
	if source != "assets/bake.txt" {
		t.Errorf("source = %q", source)
	}
}

func TestDistBakeListWithoutAssets(t *testing.T) {
	got, _, err := distBakeList(filepath.Join(t.TempDir(), "assets"), "g/assets")
	if err != nil || len(got) != 0 {
		t.Errorf("missing assets/: got %q, %v", got, err)
	}
}

// fakeBakeData は host と資産を置いた data/ を作る。
func fakeBakeData(t *testing.T) string {
	t.Helper()
	data := t.TempDir()
	writeFiles(t, data, "mitiru_host.exe", "g/g.dll", "g/assets/level.obj")
	return data
}

func envValue(env []string, key string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return strings.TrimPrefix(kv, key+"=")
		}
	}
	return ""
}

func TestBakeDistCachesSuccess(t *testing.T) {
	// 焼いた数の行は -v のときだけ出る。
	defer console.ForceVerbose(true)()
	data := fakeBakeData(t)
	t.Setenv("MITIRU_ASSET_ROOT", "somewhere-else")
	var gotArgs []string
	var gotList string
	run := func(_ context.Context, exe string, args []string, dir string, env []string) ([]byte, int, error) {
		gotArgs = args
		if exe != filepath.Join(data, "mitiru_host.exe") || dir != data {
			t.Errorf("exe=%s dir=%s", exe, dir)
		}
		if v := envValue(env, "MITIRU_ASSET_ROOT"); v != "" {
			t.Errorf("the developer's MITIRU_ASSET_ROOT leaked into the bake: %s", v)
		}
		cache := envValue(env, "MITIRU_SHADER_CACHE")
		if cache != filepath.Join(data, "shader_cache") {
			t.Errorf("MITIRU_SHADER_CACHE = %q", cache)
		}
		for i, a := range args {
			if a == "--bake-caches" {
				b, _ := os.ReadFile(args[i+1])
				gotList = string(b)
			}
		}
		writeFiles(t, cache, "a.dxbc", "b.dxbc", "c.dxbc.123.tmp")
		return []byte("[mitiru_host] bake: 1 assets, 0 failed\n"), 0, nil
	}
	var out bytes.Buffer
	if err := bakeDistCaches(data, filepath.Join("g", "g.dll"), "g", []string{"--lofi"}, run, &out); err != nil {
		t.Fatal(err)
	}
	if len(gotArgs) == 0 || gotArgs[0] != "g/g.dll" || gotArgs[len(gotArgs)-1] != "--lofi" {
		t.Errorf("args = %q", gotArgs)
	}
	if gotList != "clod:g/assets/level.obj\n" {
		t.Errorf("list = %q", gotList)
	}
	if !strings.Contains(out.String(), "baked 1 assets, 2 shaders") {
		t.Errorf("summary = %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(data, "shader_cache", "c.dxbc.123.tmp")); err == nil {
		t.Error("a half-written shader stayed in the bundle")
	}
}

func TestBakeDistCachesFailsOnUnreadableAsset(t *testing.T) {
	data := fakeBakeData(t)
	run := func(context.Context, string, []string, string, []string) ([]byte, int, error) {
		return []byte("[clod] importing level.obj\nmitiru_host: --bake-caches で読めない: g/assets/level.obj\n" +
			"[mitiru_host] bake: 1 assets, 1 failed\n"), 4, nil
	}
	err := bakeDistCaches(data, "g/g.dll", "g", nil, run, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--bake-caches で読めない: g/assets/level.obj") {
		t.Fatalf("err = %v, want the unreadable asset", err)
	}
	if strings.Contains(err.Error(), "importing") {
		t.Errorf("the error should list only the failing assets: %v", err)
	}
}

func TestBakeDistCachesWarnsWhenHostCannotRun(t *testing.T) {
	cases := map[string]distBakeRunner{
		"no GPU": func(context.Context, string, []string, string, []string) ([]byte, int, error) {
			return []byte("DX12 デバイスを作れない\n"), 1, nil
		},
		"crash": func(context.Context, string, []string, string, []string) ([]byte, int, error) {
			return nil, -1073741819, nil
		},
		"cannot start": func(context.Context, string, []string, string, []string) ([]byte, int, error) {
			return nil, 0, errors.New("access denied")
		},
	}
	for name, run := range cases {
		data := fakeBakeData(t)
		var out bytes.Buffer
		if err := bakeDistCaches(data, "g/g.dll", "g", nil, run, &out); err != nil {
			t.Errorf("%s: dist should go on without caches: %v", name, err)
		}
		if !strings.Contains(out.String(), "cache 無しで続けます") {
			t.Errorf("%s: no warning: %q", name, out.String())
		}
		if _, err := os.Stat(filepath.Join(data, "shader_cache")); err == nil {
			t.Errorf("%s: an empty shader_cache was left behind", name)
		}
	}
}

func TestBakeDistCachesSkipsWithoutHost(t *testing.T) {
	called := false
	run := func(context.Context, string, []string, string, []string) ([]byte, int, error) {
		called = true
		return nil, 0, nil
	}
	if err := bakeDistCaches(t.TempDir(), "g/g.dll", "g", nil, run, &bytes.Buffer{}); err != nil || called {
		t.Errorf("err=%v called=%v", err, called)
	}
}

func TestBakedSidecarsStayLooseWhenPacked(t *testing.T) {
	for _, rel := range []string{"level.obj.clod", "hero.fbx.glb", "island.world.json.m0.base.dds"} {
		if !keepLooseInDist(rel) {
			t.Errorf("%s would be folded into the pack, where the engine does not look for caches", rel)
		}
	}
}
