package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mogmog-0110/mitiru-cli/internal/console"
)

func writeEngineCMake(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	body := "cmake_minimum_required(VERSION 3.21)\nproject(MitiruEngine\n\tVERSION " + version + "\n\tLANGUAGES CXX)\n"
	if err := os.WriteFile(filepath.Join(root, "CMakeLists.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestEngineVersionAtReadsProjectVersion(t *testing.T) {
	root := writeEngineCMake(t, "0.38.0")
	if got := engineVersionAt(root); got != "0.38.0" {
		t.Errorf("engineVersionAt = %q; want 0.38.0", got)
	}
	if got := engineVersionAt(t.TempDir()); got != "" {
		t.Errorf("engineVersionAt without CMakeLists = %q; want empty", got)
	}
}

func TestOverrideNoticeOnlyWhenVersionsDiffer(t *testing.T) {
	defer console.ForceVerbose(false)()
	root := writeEngineCMake(t, "0.38.0")

	var same bytes.Buffer
	overrideNotice(&same, root, "v0.38.0")
	if same.Len() != 0 {
		t.Errorf("matching pin should print nothing by default, got %q", same.String())
	}

	var differ bytes.Buffer
	overrideNotice(&differ, root, "0.37.0")
	if !strings.Contains(differ.String(), "MITIRU_ENGINE_ROOT") || !strings.Contains(differ.String(), "0.38.0") {
		t.Errorf("different pin should be reported with both versions, got %q", differ.String())
	}
}
