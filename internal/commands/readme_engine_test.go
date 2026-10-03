package commands

import (
	"os"
	"regexp"
	"testing"
)

// README の mitiru.toml の例は、mitiru new が書く engine の版と同じであること。
// 既定の版を上げたときに README の例だけ古いまま残らないようにする。
func TestReadmeManifestExamplePinsTheDefaultEngine(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^engine = "([^"]+)"`).FindSubmatch(b)
	if m == nil {
		t.Fatal(`README.md has no 'engine = "..."' example`)
	}
	if string(m[1]) != defaultEngineVersion {
		t.Errorf("README.md shows engine = %q; mitiru new writes %q", m[1], defaultEngineVersion)
	}
}
