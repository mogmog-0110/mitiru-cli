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

	"github.com/mogmog-0110/mitiru-cli/internal/console"
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

// distBakeDawnPasses は Dawn の変換結果を焼く host の起動。D3D12 の無い PC が落ちる先は Dawn の D3D11 で、
// 通常の水準と互換の水準 (FL 11_0 の GPU) で pipeline の鍵が別になるので、両方走らせる。
var distBakeDawnPasses = []struct {
	name string
	env  []string
}{
	{"d3d11", nil},
	{"d3d11 compat", []string{"MITIRU_DAWN_COMPAT=1"}},
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
// dawn が真なら (features に dawn)、DX12 の後に Dawn の変換結果も焼く。こちらは失敗しても知らせるだけで dist を止めない。
func bakeDistCaches(dataDir, dllRel, gameDir string, hostArgs []string, dawn bool, run distBakeRunner, out io.Writer) error {
	host := filepath.Join(dataDir, "mitiru_host.exe")
	if _, err := os.Stat(host); err != nil {
		console.Fverbosef(out, "dist: no mitiru_host.exe; skipping the cache bake\n")
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

	shaderDir := filepath.Join(dataDir, "shader_cache")
	job := distBakeJob{host: host, dataDir: dataDir, dllRel: dllRel, tmp: tmp, shaderDir: shaderDir, hostArgs: hostArgs, run: run}
	err = job.bakeDX12(entries, source, out)
	if err == nil && dawn {
		job.bakeDawn(entries, out)
	}
	if err == nil {
		tidyShaderCache(shaderDir)
	}
	return err
}

// distBakeJob は 1 回の dist の焼き込みで、どの起動にも共通の物。
type distBakeJob struct {
	host, dataDir, dllRel, tmp, shaderDir string
	hostArgs                              []string
	run                                   distBakeRunner
}

// launch は一覧を書き、host を 1 回走らせる。セーブと設定は一時フォルダへ逃がす (焼くために数フレーム走らせたゲームの書き物を配布物に残さない)。
// --headless は --bake-caches が自分で立てるが、窓を出さない起動だと引数からも分かるように付けておく。
func (j distBakeJob) launch(tag string, list []string, extraArgs, extraEnv []string) ([]byte, int, float64, error) {
	listFile := filepath.Join(j.tmp, tag+"-bake.txt")
	if err := os.WriteFile(listFile, []byte(strings.Join(list, "\n")+"\n"), 0o644); err != nil {
		return nil, 0, 0, fmt.Errorf("dist: bake: write list: %w", err)
	}
	args := append([]string{filepath.ToSlash(j.dllRel), "--headless", "--bake-caches", listFile,
		"--save-dir", filepath.Join(j.tmp, tag+"-save"), "--settings", filepath.Join(j.tmp, tag+"-settings.json")}, j.hostArgs...)
	args = append(args, extraArgs...)
	env := append(withoutMitiruEnv(os.Environ()), "MITIRU_SHADER_CACHE="+j.shaderDir)
	env = append(env, extraEnv...)

	ctx, cancel := context.WithTimeout(context.Background(), distBakeTimeout)
	defer cancel()
	start := time.Now()
	output, code, runErr := j.run(ctx, j.host, args, j.dataDir, env)
	return output, code, time.Since(start).Seconds(), runErr
}

func (j distBakeJob) bakeDX12(entries []string, source string, out io.Writer) error {
	output, code, elapsed, runErr := j.launch("dx12", entries, nil, nil)
	shaders := tidyShaderCache(j.shaderDir)
	switch {
	case runErr != nil:
		fmt.Fprintf(out, "読み込みの cache を作る host を起動できませんでした (%v)。cache 無しで続けます。\n", runErr)
	case code == 4:
		return fmt.Errorf("読めない資産があります。このまま配ると、遊ぶ側でも読めません。\n  %s",
			strings.Join(bakeFailLines(string(output)), "\n  "))
	case code != 0:
		fmt.Fprintf(out, "読み込みの cache を作れませんでした (終了コード %s)。cache 無しで続けます。\n  %s\n",
			hostExitHint(code), lastLine(string(output)))
	default:
		console.Fverbosef(out, "dist: baked %d assets, %d shaders (%s, %.1fs)\n", len(entries), shaders, source, elapsed)
	}
	return nil
}

// bakeDawn は Dawn の D3D11 で host を走らせ、パイプラインの変換結果を同じ置き場へ残す。
// DX12 だけが読める資産 (clod・world・region・焼いた光) は Dawn では読めず、読めないのは異常でないので、モデルだけを渡す。
// 鍵は Dawn の版と GPU の組で決まる。遊ぶ側の GPU と合わなければ cache は使われないだけで、読み込みは変換し直す。
func (j distBakeJob) bakeDawn(entries []string, out io.Writer) {
	models := dawnBakeEntries(entries)
	for i, pass := range distBakeDawnPasses {
		before := countShaderFiles(j.shaderDir)
		// セーブと設定は起動ごとに分ける。前の起動の書き物を次が読むと、互換の水準が独立しなくなる
		output, code, elapsed, runErr := j.launch(fmt.Sprintf("dawn%d", i), models, []string{"--backend", "dawn-d3d11"}, pass.env)
		switch {
		case runErr != nil:
			fmt.Fprintf(out, "Dawn (%s) の cache を作る host を起動できませんでした (%v)。Dawn の cache 無しで続けます。\n", pass.name, runErr)
		case code != 0:
			fmt.Fprintf(out, "Dawn (%s) の cache を作れませんでした (終了コード %s)。Dawn の cache 無しで続けます。\n  %s\n",
				pass.name, hostExitHint(code), lastLine(string(output)))
		default:
			console.Fverbosef(out, "dist: baked Dawn %s, +%d files (%.1fs)\n", pass.name, countShaderFiles(j.shaderDir)-before, elapsed)
		}
	}
}

// dawnBakeEntries は一覧のうち Dawn が読めるモデルの行だけを返す。
func dawnBakeEntries(entries []string) []string {
	var out []string
	for _, entry := range entries {
		kind, path := splitBakeKind(entry)
		if kind == "model" || (kind == "" && distBakeModelExt[strings.ToLower(filepath.Ext(path))] == "model") {
			out = append(out, entry)
		}
	}
	return out
}

// splitBakeKind は engine の splitAssetKind と同じく、行頭の "model:" "clod:" "sound:" を種類として切る。
func splitBakeKind(entry string) (kind, path string) {
	for _, k := range []string{"model", "clod", "sound"} {
		if rest, ok := strings.CutPrefix(entry, k+":"); ok {
			return k, strings.TrimSpace(rest)
		}
	}
	return "", entry
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

// countShaderFiles は置き場にあるシェーダーの数を返す (書きかけは数えない)。
func countShaderFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.Name()), ".dxbc") {
			n++
		}
	}
	return n
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
