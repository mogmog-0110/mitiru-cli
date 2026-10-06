package build

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// sccache の server が端末の無い (OEM コードページの) 環境で cl を走らせると、日本語 Windows では
// /showIncludes の接頭辞が化ける。ninja は configure で控えた UTF-8 の接頭辞とバイト列で比べて
// header の依存を拾うので、合わないと依存が 1 件も記録されず、header を直しても組み直されなくなる。
// cl は server の中で動くので chcp では揃えられない。server を UTF-8 の端末の中で起こせば揃う。
// 起こす前に probe で今の server の出力を直の cl と比べ、違うときだけ起こし直す。

const probeSource = "#include \"probe.h\"\nint main() { return PROBE_VALUE + PROBE_NONCE; }\n"

// showIncludesPrefix は cl の /showIncludes の出力から、ヘッダの path の手前までの接頭辞を取り出す。
func showIncludesPrefix(output, headerDir string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if i := strings.Index(line, headerDir); i > 0 {
			return strings.TrimRight(line[:i], " "), true
		}
	}
	return "", false
}

func cacheServerMatchesCompiler(direct, cached, dir string) bool {
	want, ok := showIncludesPrefix(direct, dir)
	if !ok {
		return false
	}
	got, ok := showIncludesPrefix(cached, dir)
	return ok && got == want
}

// ensureCacheServer は sccache を使うときだけ、server の cl の出力が直の cl と同じかを確かめ、違えば起こし直す。
func ensureCacheServer(vcvars string, opts Options) error {
	if os.Getenv("MITIRU_DRY_RUN") == "1" {
		return nil
	}
	exe := findSccache(opts.Cache)
	if exe == "" {
		return nil
	}
	if err := clearStaleCache(exe); err != nil {
		return err
	}
	ok, err := probeCacheServer(vcvars, exe, opts)
	if err != nil || ok {
		return err
	}
	if err := restartCacheServer(exe); err != nil {
		return err
	}
	if ok, err = probeCacheServer(vcvars, exe, opts); err != nil || ok {
		return err
	}
	return fmt.Errorf("sccache 越しの cl の出力が直の cl と合いません。header の依存が記録されないので、MITIRU_SCCACHE=off か mitiru.toml の [build] cache = \"none\" で sccache を切ってください。")
}

func probeCacheServer(vcvars, exe string, opts Options) (bool, error) {
	dir, err := os.MkdirTemp("", "mitiru_probe-")
	if err != nil {
		return false, fmt.Errorf("create probe dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	files := map[string]string{
		"probe.cpp": probeSource,
		"probe.h":   "#define PROBE_VALUE 0\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return false, fmt.Errorf("write probe: %w", err)
		}
	}
	// 毎回 miss させる。hit だと過去に控えた出力 (別の dir の path) を見てしまう。
	// -D は前処理の結果に残らず hash に入らないので、source の中で使って前処理の結果を変える
	cl := fmt.Sprintf("cl /nologo /showIncludes /Z7 /c probe.cpp /DPROBE_NONCE=%d /Fo", time.Now().UnixNano())
	script := vcvarsPrelude(vcvars) +
		"cd /d \"" + dir + "\"\r\n" +
		cl + "direct.obj > direct.txt 2>&1\r\n" +
		"\"" + filepath.FromSlash(exe) + "\" " + cl + "cached.obj > cached.txt 2>&1\r\n"
	quiet := opts
	quiet.Stdout, quiet.Stderr = &bytes.Buffer{}, &bytes.Buffer{}
	if err := runBatchScript("mitiru_probe", script, quiet); err != nil {
		return false, fmt.Errorf("sccache の確認に失敗しました: %w", err)
	}
	direct, _ := os.ReadFile(filepath.Join(dir, "direct.txt"))
	cached, _ := os.ReadFile(filepath.Join(dir, "cached.txt"))
	return cacheServerMatchesCompiler(string(direct), string(cached), dir), nil
}

func restartCacheServer(exe string) error {
	_ = exec.Command(exe, "--stop-server").Run() // 動いていなければ失敗するが、それでよい
	if err := startUTF8CacheServer(exe); err != nil {
		return fmt.Errorf("sccache の server を起こせませんでした: %w", err)
	}
	for i := 0; i < 50; i++ {
		if exec.Command(exe, "--show-stats").Run() == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("sccache の server が応えません")
}
