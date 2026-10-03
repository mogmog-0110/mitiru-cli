package console

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerWritesOnlyWhenVerbose(t *testing.T) {
	var quiet, loud bytes.Buffer
	NewLogger(&quiet, false).Printf("Building %s\n", "probe")
	NewLogger(&loud, true).Printf("Building %s\n", "probe")

	if quiet.Len() != 0 {
		t.Errorf("quiet logger wrote %q; want nothing", quiet.String())
	}
	if loud.String() != "Building probe\n" {
		t.Errorf("verbose logger wrote %q; want %q", loud.String(), "Building probe\n")
	}
}

func TestFverbosefFollowsGlobalFlag(t *testing.T) {
	prev := verbose.Load()
	defer verbose.Store(prev)

	var out bytes.Buffer
	verbose.Store(false)
	Fverbosef(&out, "hidden\n")
	if out.Len() != 0 {
		t.Fatalf("Fverbosef wrote %q without -v", out.String())
	}

	SetVerbose(true)
	Fverbosef(&out, "shown\n")
	if out.String() != "shown\n" {
		t.Fatalf("Fverbosef wrote %q with -v; want %q", out.String(), "shown\n")
	}

	// -v を付けなかったときの false で、環境変数による有効を消さない。
	SetVerbose(false)
	if !Verbose() {
		t.Fatal("SetVerbose(false) turned verbose off")
	}
}

func TestVerboseFromEnv(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want bool
	}{
		{map[string]string{}, false},
		{map[string]string{"MITIRU_LOG": "verbose"}, true},
		{map[string]string{"MITIRU_LOG": "VERBOSE"}, true},
		{map[string]string{"MITIRU_LOG": "quiet"}, false},
		{map[string]string{"MITIRU_VERBOSE": "1"}, true},
	}
	for _, c := range cases {
		got := verboseFromEnv(func(k string) string { return c.env[k] })
		if got != c.want {
			t.Errorf("verboseFromEnv(%v) = %v; want %v", c.env, got, c.want)
		}
	}
}

func TestChildEnvAddsLogOnlyWhenVerbose(t *testing.T) {
	prev := verbose.Load()
	defer verbose.Store(prev)

	base := []string{"PATH=C:\\bin", "MITIRU_LOG=quiet"}

	verbose.Store(false)
	if got := ChildEnv(base); strings.Join(got, ";") != strings.Join(base, ";") {
		t.Errorf("ChildEnv without -v changed env: %v", got)
	}

	verbose.Store(true)
	got := ChildEnv(base)
	joined := strings.Join(got, ";")
	if !strings.Contains(joined, "MITIRU_LOG=verbose") || strings.Contains(joined, "MITIRU_LOG=quiet") {
		t.Errorf("ChildEnv with -v = %v; want a single MITIRU_LOG=verbose", got)
	}
	if base[1] != "MITIRU_LOG=quiet" {
		t.Error("ChildEnv modified its input slice")
	}
}
