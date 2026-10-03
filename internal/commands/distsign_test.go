package commands

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fakeEnv(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func noLookPath(string) (string, error) { return "", errors.New("not on PATH") }

func TestSigntoolArgsPfx(t *testing.T) {
	cfg := signConfig{Tool: "signtool.exe", CertFile: `C:\k\me.pfx`, Password: "hunter2",
		TimestampURL: defaultTimestamp}
	got := signtoolArgs(cfg, []string{"a.exe", "b.dll"})
	want := []string{"sign", "/fd", "SHA256", "/td", "SHA256", "/tr", "http://timestamp.digicert.com",
		"/f", `C:\k\me.pfx`, "/p", "hunter2", "a.exe", "b.dll"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("signtoolArgs =\n %q\nwant\n %q", got, want)
	}
}

func TestSigntoolArgsThumbprintWithoutPassword(t *testing.T) {
	cfg := signConfig{Thumbprint: "ABCDEF", TimestampURL: "http://ts.example"}
	got := signtoolArgs(cfg, []string{"a.exe"})
	want := []string{"sign", "/fd", "SHA256", "/td", "SHA256", "/tr", "http://ts.example",
		"/sha1", "ABCDEF", "a.exe"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("signtoolArgs =\n %q\nwant\n %q", got, want)
	}
}

func TestMaskSignArgsHidesPasswordOnly(t *testing.T) {
	args := []string{"sign", "/f", "me.pfx", "/p", "hunter2", "a.exe"}
	got := maskSignArgs(args)
	if strings.Contains(strings.Join(got, " "), "hunter2") || got[4] != "***" {
		t.Errorf("password must be masked: %q", got)
	}
	if args[4] != "hunter2" {
		t.Errorf("maskSignArgs must not change the args passed to signtool: %q", args)
	}
}

// signtool を走らせずに、渡るコマンド行と画面に出る行を確かめる。
func TestSignFilesDryRun(t *testing.T) {
	cfg := signConfig{Tool: "signtool.exe", CertFile: "me.pfx", Password: "hunter2", TimestampURL: defaultTimestamp}
	var gotTool string
	var gotArgs []string
	run := func(tool string, args []string) error { gotTool, gotArgs = tool, args; return nil }
	var echo bytes.Buffer
	if err := signFiles(cfg, []string{"g.exe", "g.dll"}, run, &echo); err != nil {
		t.Fatal(err)
	}
	if gotTool != "signtool.exe" || !reflect.DeepEqual(gotArgs, signtoolArgs(cfg, []string{"g.exe", "g.dll"})) {
		t.Errorf("runner got %s %q", gotTool, gotArgs)
	}
	if strings.Contains(echo.String(), "hunter2") || !strings.Contains(echo.String(), "/p ***") {
		t.Errorf("echoed command line must mask the password: %q", echo.String())
	}
}

func TestLoadSignConfigMissingSigntool(t *testing.T) {
	pf := t.TempDir()
	_, err := loadSignConfig(fakeEnv(map[string]string{"ProgramFiles(x86)": pf,
		envSignThumbprint: "ABCDEF"}), noLookPath)
	if err == nil || !strings.Contains(err.Error(), "Windows SDK") || !strings.Contains(err.Error(), envSigntool) {
		t.Errorf("missing signtool should say where it looked and that it comes with the SDK: %v", err)
	}
	_, err = loadSignConfig(fakeEnv(map[string]string{envSigntool: filepath.Join(pf, "nope.exe")}), noLookPath)
	if err == nil || !strings.Contains(err.Error(), envSigntool) {
		t.Errorf("a MITIRU_SIGNTOOL that does not exist must fail: %v", err)
	}
}

func TestLoadSignConfigPicksNewestSDK(t *testing.T) {
	pf := t.TempDir()
	for _, v := range []string{"10.0.9.0", "10.0.22621.0", "10.0.19041.0"} {
		writeTestFile(t, filepath.Join(pf, "Windows Kits", "10", "bin", v, "x64", "signtool.exe"), "x")
	}
	cfg, err := loadSignConfig(fakeEnv(map[string]string{"ProgramFiles(x86)": pf,
		envSignThumbprint: "ABCDEF"}), noLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.Tool, "10.0.22621.0") || cfg.TimestampURL != defaultTimestamp {
		t.Errorf("tool = %s, ts = %s; want the newest SDK and the default timestamp", cfg.Tool, cfg.TimestampURL)
	}
}

func TestLoadSignConfigCertErrors(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "signtool.exe")
	writeTestFile(t, tool, "x")
	cases := map[string]map[string]string{
		"no cert":     {envSigntool: tool},
		"missing pfx": {envSigntool: tool, envSignCertFile: filepath.Join(t.TempDir(), "me.pfx")},
		"both certs":  {envSigntool: tool, envSignCertFile: tool, envSignThumbprint: "ABCDEF"},
	}
	for name, env := range cases {
		if _, err := loadSignConfig(fakeEnv(env), noLookPath); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	pfx := filepath.Join(t.TempDir(), "me.pfx")
	writeTestFile(t, pfx, "x")
	cfg, err := loadSignConfig(fakeEnv(map[string]string{envSigntool: tool, envSignCertFile: pfx,
		envSignCertPass: "pw", envSignTimestamp: "http://ts.example"}), noLookPath)
	if err != nil || cfg.CertFile != pfx || cfg.Password != "pw" || cfg.TimestampURL != "http://ts.example" {
		t.Errorf("valid pfx config: %+v, %v", cfg, err)
	}
}

func TestDistSignTargetsOwnBinariesOnly(t *testing.T) {
	bundle := t.TempDir()
	data := filepath.Join(bundle, "data")
	for _, rel := range []string{"g.exe", "data/mitiru_host.exe", "data/g.exe", "data/SDL3.dll",
		"data/vcruntime140.dll", "data/g/g.dll", "data/g/assets/x.png"} {
		writeTestFile(t, filepath.Join(bundle, filepath.FromSlash(rel)), "x")
	}
	got, err := distSignTargets(bundle, data, "g", "g", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(bundle, "g.exe"), filepath.Join(data, "mitiru_host.exe"),
		filepath.Join(data, "g.exe"), filepath.Join(data, "g", "g.dll")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("targets =\n %q\nwant\n %q", got, want)
	}
}
