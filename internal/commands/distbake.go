package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// distNoBake は配布前の cache の焼き込み (mitiru_host --bake-caches) を飛ばす。
var distNoBake bool

// distBakeListFile はゲームが焼く資産を自分で決めるときの一覧。assets/ からの相対。
const distBakeListFile = "bake.txt"

// distBakeTimeout を過ぎたら host を止める。初回の変換が大きなモデルでも数分で終わる長さ。
const distBakeTimeout = 10 * time.Minute

// distBakeFailMark は host が読めなかった資産を知らせる行の頭。engine の HostStreaming に合わせてある。
const distBakeFailMark = "--bake-caches で読めない"

// distBakeModelExt は読み込みの cache を作るモデルの拡張子と、一覧に付ける種類。
// glTF / FBX はスキンのモデルとして読む方を既定にし、drawModel の世界のモデルにしたいものは bake.txt に clod: で書く。
var distBakeModelExt = map[string]string{
	".glb": "model", ".gltf": "model", ".fbx": "model", ".vrm": "model",
	".obj": "clod", ".clod": "clod",
}

// distBakeRunner は host を走らせ、出力と終了コードを返す。err は起動できなかったときだけ。
type distBakeRunner func(ctx context.Context, exe string, args []string, dir string, env []string) ([]byte, int, error)

func execDistBakeRunner(ctx context.Context, exe string, args []string, dir string, env []string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out, exitErr.ExitCode(), nil
	}
	return out, 0, err
}

// distBakeList は焼く資産の一覧と、どこから決めたかを返す。assets/bake.txt があればその行をそのまま使い、
// 無ければ assets/ を走査する。パスは host の作業フォルダ (data/) から見た "<gameDir>/assets/..."。
func distBakeList(assetsDir, logicalPrefix string) ([]string, string, error) {
	if text, err := os.ReadFile(filepath.Join(assetsDir, distBakeListFile)); err == nil {
		return parseBakeList(string(text)), "assets/" + distBakeListFile, nil
	} else if !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("dist: read %s: %w", distBakeListFile, err)
	}
	entries, err := scanBakeAssets(assetsDir, logicalPrefix)
	return entries, "assets/ の走査", err
}

// parseBakeList は engine の parseAssetList と同じく、# から後と空の行を落とす。
func parseBakeList(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func scanBakeAssets(assetsDir, logicalPrefix string) ([]string, error) {
	var out []string
	err := filepath.Walk(assetsDir, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			if os.IsNotExist(werr) && path == assetsDir {
				return filepath.SkipDir
			}
			return werr
		}
		rel, rerr := filepath.Rel(assetsDir, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			if rel == packSkipDir {
				return filepath.SkipDir
			}
			return nil
		}
		if entry := bakeEntryFor(rel); entry != "" {
			out = append(out, strings.Replace(entry, "@", logicalPrefix+"/"+rel, 1))
		}
		return nil
	})
	return out, err
}

// bakeEntryFor は一覧の 1 行の形 (パスの所は @) を返す。焼かないファイルは空。
func bakeEntryFor(rel string) string {
	low := strings.ToLower(rel)
	// 焼いた光は cache を作らないが、読ませておけば壊れた .lighting.bin を配る前に止められる
	if strings.HasSuffix(low, ".world.json") || strings.HasSuffix(low, ".region.json") ||
		strings.HasSuffix(low, ".lighting.bin") {
		return "@"
	}
	ext := filepath.Ext(low)
	kind, ok := distBakeModelExt[ext]
	if !ok {
		return ""
	}
	// x.fbx.glb や x.obj.clod は engine が作った cache で、元のモデルを読めば作り直される
	if _, inner := distBakeModelExt[filepath.Ext(strings.TrimSuffix(low, ext))]; inner {
		return ""
	}
	return kind + ":@"
}

// bakeDistCaches は配布物の中で host に資産を読ませ、変換の cache・DDS・シェーダーの cache を作らせる。
// 遊ぶ側の初回起動で止まらず、書けない場所に入れても動くようにするため。
// 読めない資産があれば失敗にする。host が走れない (DX12 の GPU が無い等) ときは知らせて cache 無しで続ける。
func bakeDistCaches(dataDir, dllRel, gameDir string, hostArgs []string, run distBakeRunner, out io.Writer) error {
	host := filepath.Join(dataDir, "mitiru_host.exe")
	if _, err := os.Stat(host); err != nil {
		fmt.Fprintln(out, "dist: mitiru_host.exe が無いので cache の焼き込みを飛ばす")
		return nil
	}
	entries, source, err := distBakeList(filepath.Join(dataDir, gameDir, "assets"), gameDir+"/assets")
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "mitiru-dist-bake-")
	if err != nil {
		return fmt.Errorf("dist: bake: %w", err)
	}
	defer os.RemoveAll(tmp)
	list := filepath.Join(tmp, "bake.txt")
	if err := os.WriteFile(list, []byte(strings.Join(entries, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("dist: bake: write list: %w", err)
	}

	shaderDir := filepath.Join(dataDir, "shader_cache")
	// セーブと設定は一時フォルダへ逃がす。焼くために 2 フレーム走らせたゲームの書き物を配布物に残さない
	// --headless は --bake-caches が自分で立てるが、窓を出さない起動だと引数からも分かるように付けておく
	args := append([]string{filepath.ToSlash(dllRel), "--headless", "--bake-caches", list,
		"--save-dir", filepath.Join(tmp, "save"), "--settings", filepath.Join(tmp, "settings.json")}, hostArgs...)
	env := append(withoutMitiruEnv(os.Environ()), "MITIRU_SHADER_CACHE="+shaderDir)

	ctx, cancel := context.WithTimeout(context.Background(), distBakeTimeout)
	defer cancel()
	start := time.Now()
	output, code, runErr := run(ctx, host, args, dataDir, env)
	elapsed := time.Since(start).Seconds()
	shaders := tidyShaderCache(shaderDir)

	switch {
	case runErr != nil:
		fmt.Fprintf(out, "dist: warning: cache を焼く host を起動できない (cache 無しで続ける): %v\n", runErr)
		return nil
	case code == 4:
		return fmt.Errorf("dist: 読めない資産がある (このまま配ると遊ぶ側でも読めない)\n  %s",
			strings.Join(bakeFailLines(string(output)), "\n  "))
	case code != 0:
		fmt.Fprintf(out, "dist: warning: cache を焼けなかった (終了コード %s)。cache 無しで続ける\n  %s\n",
			hostExitHint(code), lastLine(string(output)))
		return nil
	}
	fmt.Fprintf(out, "dist: baked %d assets, %d shaders (%s, %.1fs)\n", len(entries), shaders, source, elapsed)
	return nil
}

func withoutMitiruEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if !strings.HasPrefix(strings.ToUpper(kv), "MITIRU_") {
			out = append(out, kv)
		}
	}
	return out
}

// tidyShaderCache は書きかけの一時ファイルと空の置き場を消し、残ったシェーダーの数を返す。
func tidyShaderCache(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		switch name := strings.ToLower(e.Name()); {
		case strings.HasSuffix(name, ".tmp"):
			_ = os.Remove(filepath.Join(dir, e.Name()))
		case strings.HasSuffix(name, ".dxbc"):
			n++
		}
	}
	if n == 0 {
		_ = os.RemoveAll(dir)
	}
	return n
}

func bakeFailLines(output string) []string {
	var out []string
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); strings.Contains(line, distBakeFailMark) {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		out = append(out, lastLine(output))
	}
	return out
}

func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
