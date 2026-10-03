package build

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigureKeyChangesWithEachInput(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "build", "cmake")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(src, "CMakeLists.txt"), "project(a)\n")
	writeTestFile(t, filepath.Join(root, "mitiru.toml"), "[project]\nengine = \"0.38.0\"\n")
	in := configureInputs{Command: "cmake -S a -B b -G Ninja -DCMAKE_BUILD_TYPE=Debug", SrcDir: src, ManifestDir: root}
	base := configureKey(in)
	if base != configureKey(in) {
		t.Fatal("same inputs must give the same key")
	}

	release := in
	release.Command = "cmake -S a -B b -G Ninja -DCMAKE_BUILD_TYPE=Release"
	if configureKey(release) == base {
		t.Error("a different cmake command line must change the key")
	}

	writeTestFile(t, filepath.Join(root, "mitiru.toml"), "[project]\nengine = \"0.39.0\"\n")
	if configureKey(in) == base {
		t.Error("editing mitiru.toml must change the key")
	}
	writeTestFile(t, filepath.Join(root, "mitiru.toml"), "[project]\nengine = \"0.38.0\"\n")

	writeTestFile(t, filepath.Join(src, "CMakeLists.txt"), "project(b)\n")
	if configureKey(in) == base {
		t.Error("a different generated CMakeLists.txt must change the key")
	}
}

func TestToolsetNamesFollowsTheVSLayout(t *testing.T) {
	vs := t.TempDir()
	build := filepath.Join(vs, "VC", "Auxiliary", "Build")
	for _, d := range []string{build, filepath.Join(vs, "VC", "Tools", "MSVC", "14.44.35207")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := toolsetNames(filepath.Join(build, "vcvars64.bat"))
	if len(got) != 1 || got[0] != "14.44.35207" {
		t.Fatalf("toolsetNames = %v, want [14.44.35207]", got)
	}
}

func TestConfigureUpToDateNeedsCacheAndMatchingStamp(t *testing.T) {
	out := t.TempDir()
	if err := writeConfigureStamp(out, "k1"); err != nil {
		t.Fatal(err)
	}
	if configureUpToDate(out, "k1") {
		t.Error("without CMakeCache.txt the build dir was never configured")
	}
	writeTestFile(t, filepath.Join(out, "CMakeCache.txt"), "")
	if !configureUpToDate(out, "k1") {
		t.Error("matching stamp and an existing cache must skip configure")
	}
	if configureUpToDate(out, "k2") {
		t.Error("a different key must configure again")
	}
	clearConfigureStamp(out)
	if configureUpToDate(out, "k1") {
		t.Error("a cleared stamp must configure again")
	}
}

func TestWriteFileIfChangedKeepsModTime(t *testing.T) {
	p := filepath.Join(t.TempDir(), "CMakeLists.txt")
	writeTestFile(t, p, "same\n")
	old := mustModTime(t, p)
	// 時刻の分解能より前の時刻にしておき、書き直されたら必ず差が出るようにする
	past := old.Add(-10 * time.Second)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatal(err)
	}
	if err := writeFileIfChanged(p, []byte("same\n")); err != nil {
		t.Fatal(err)
	}
	if !mustModTime(t, p).Equal(past) {
		t.Error("identical content must not touch the file (ninja would re-run cmake)")
	}
	if err := writeFileIfChanged(p, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if mustModTime(t, p).Equal(past) {
		t.Error("changed content must be written")
	}
}

func mustModTime(t *testing.T, p string) time.Time {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.ModTime()
}
