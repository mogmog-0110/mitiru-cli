package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mogmog-0110/mitiru-cli/internal/console"
)

// 署名の設定は環境変数だけから読む。証明書の場所やパスワードを mitiru.toml に書かせると、
// repo に入って人の手へ渡るからだ。
//
//	MITIRU_SIGNTOOL              signtool.exe の場所。無ければ PATH、次に Windows SDK の中を探す
//	MITIRU_SIGN_CERT_FILE        .pfx の場所 (MITIRU_SIGN_CERT_PASSWORD でパスワード)
//	MITIRU_SIGN_CERT_THUMBPRINT  Windows の証明書ストアにある証明書の拇印 (/sha1)
//	MITIRU_SIGN_TIMESTAMP_URL    タイムスタンプ局。既定は http://timestamp.digicert.com
const (
	envSigntool        = "MITIRU_SIGNTOOL"
	envSignCertFile    = "MITIRU_SIGN_CERT_FILE"
	envSignCertPass    = "MITIRU_SIGN_CERT_PASSWORD"
	envSignThumbprint  = "MITIRU_SIGN_CERT_THUMBPRINT"
	envSignTimestamp   = "MITIRU_SIGN_TIMESTAMP_URL"
	defaultTimestamp   = "http://timestamp.digicert.com"
	signPasswordMasked = "***"
)

// distSign は自分たちの exe と DLL に Authenticode 署名を付ける。
var distSign bool

type signConfig struct {
	Tool         string
	CertFile     string
	Password     string
	Thumbprint   string
	TimestampURL string
}

// signRunner は signtool を走らせる。テストでは差し替えて、signtool 無しでコマンド行だけを確かめる。
type signRunner func(tool string, args []string) error

var distSignRunner signRunner = func(tool string, args []string) error {
	cmd := exec.Command(tool, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// loadSignConfig は --sign の設定を環境変数から読み、ビルドの前に足りないものを知らせる。
// 10 分かけてビルドしてから署名で落ちるのを避けるためだ。
func loadSignConfig(getenv func(string) string, lookPath func(string) (string, error)) (signConfig, error) {
	tool, err := findSigntool(getenv, lookPath)
	if err != nil {
		return signConfig{}, err
	}
	cfg := signConfig{
		Tool:         tool,
		CertFile:     strings.TrimSpace(getenv(envSignCertFile)),
		Password:     getenv(envSignCertPass),
		Thumbprint:   strings.TrimSpace(getenv(envSignThumbprint)),
		TimestampURL: strings.TrimSpace(getenv(envSignTimestamp)),
	}
	if cfg.TimestampURL == "" {
		cfg.TimestampURL = defaultTimestamp
	}
	switch {
	case cfg.CertFile != "" && cfg.Thumbprint != "":
		return signConfig{}, fmt.Errorf("dist --sign: %s と %s の両方がある。どちらの証明書で署名するか決められないので片方だけにする",
			envSignCertFile, envSignThumbprint)
	case cfg.CertFile != "":
		if _, err := os.Stat(cfg.CertFile); err != nil {
			return signConfig{}, fmt.Errorf("dist --sign: %s の証明書 %s が無い: %w", envSignCertFile, cfg.CertFile, err)
		}
	case cfg.Thumbprint == "":
		return signConfig{}, fmt.Errorf("dist --sign: 証明書が指定されていない。%s に .pfx の場所 (パスワードは %s) か、"+
			"%s に証明書ストアの拇印を入れる", envSignCertFile, envSignCertPass, envSignThumbprint)
	}
	return cfg, nil
}

// findSigntool は MITIRU_SIGNTOOL、PATH、Windows SDK の最新版の順に signtool.exe を探す。
func findSigntool(getenv func(string) string, lookPath func(string) (string, error)) (string, error) {
	if explicit := strings.TrimSpace(getenv(envSigntool)); explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("dist --sign: %s の %s が無い: %w", envSigntool, explicit, err)
		}
		return explicit, nil
	}
	if p, err := lookPath("signtool.exe"); err == nil {
		return p, nil
	}
	sdkBin := filepath.Join(getenv("ProgramFiles(x86)"), "Windows Kits", "10", "bin")
	if p := newestSDKSigntool(sdkBin); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("dist --sign: signtool.exe が見つからない。%s (未設定)、PATH、%s を探した。"+
		"signtool は Windows SDK に入っているので、SDK を入れるか %s で場所を指す",
		envSigntool, filepath.Join(sdkBin, "<版>", "x64", "signtool.exe"), envSigntool)
}

// newestSDKSigntool は sdkBin/<版>/x64/signtool.exe のうち版の一番新しいものを返す。無ければ空。
func newestSDKSigntool(sdkBin string) string {
	entries, err := os.ReadDir(sdkBin)
	if err != nil {
		return ""
	}
	var found []string
	for _, e := range entries {
		p := filepath.Join(sdkBin, e.Name(), "x64", "signtool.exe")
		if _, serr := os.Stat(p); e.IsDir() && serr == nil {
			found = append(found, e.Name())
		}
	}
	if len(found) == 0 {
		return ""
	}
	// 文字列で比べると 10.0.9.0 が 10.0.22621.0 より新しく見えるので、数として比べる
	sort.Slice(found, func(i, j int) bool { return versionLess(found[i], found[j]) })
	return filepath.Join(sdkBin, found[len(found)-1], "x64", "signtool.exe")
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		na, nb := versionPart(pa, i), versionPart(pb, i)
		if na != nb {
			return na < nb
		}
	}
	return false
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return -1
	}
	return n
}

// signtoolArgs は signtool に渡す引数を作る。SHA256 のダイジェストと RFC 3161 のタイムスタンプを付ける。
func signtoolArgs(cfg signConfig, files []string) []string {
	args := []string{"sign", "/fd", "SHA256", "/td", "SHA256", "/tr", cfg.TimestampURL}
	if cfg.CertFile != "" {
		args = append(args, "/f", cfg.CertFile)
		if cfg.Password != "" {
			args = append(args, "/p", cfg.Password)
		}
	} else {
		args = append(args, "/sha1", cfg.Thumbprint)
	}
	return append(args, files...)
}

// maskSignArgs は画面とログに出すために /p の値を伏せた写しを返す。
func maskSignArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i+1 < len(out); i++ {
		if strings.EqualFold(out[i], "/p") {
			out[i+1] = signPasswordMasked
		}
	}
	return out
}

// signFiles は files に署名する。走らせるコマンド行は w に出すが、パスワードは伏せる。
func signFiles(cfg signConfig, files []string, run signRunner, w io.Writer) error {
	if len(files) == 0 {
		return errors.New("dist --sign で署名するファイルがありません。")
	}
	args := signtoolArgs(cfg, files)
	console.Fverbosef(w, "dist --sign: %s %s\n", cfg.Tool, strings.Join(maskSignArgs(args), " "))
	if err := run(cfg.Tool, args); err != nil {
		return fmt.Errorf("signtool での署名に失敗しました (%w)。", err)
	}
	return nil
}

// distSignTargets は bundle のうち自分たちが作ったバイナリだけを返す。SDL3.dll や VC ランタイムなど
// 他者のバイナリに自分の名前で署名すると、その中身を自分が保証することになるので外す。
func distSignTargets(bundleRoot, dataDir, name, gameDir string, withExe bool) ([]string, error) {
	var files []string
	for _, p := range []string{filepath.Join(bundleRoot, name+".exe"), filepath.Join(dataDir, "mitiru_host.exe")} {
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
	}
	if withExe {
		files = append(files, filepath.Join(dataDir, name+".exe"))
	}
	dlls, err := filepath.Glob(filepath.Join(dataDir, gameDir, "*.dll"))
	if err != nil {
		return nil, fmt.Errorf("dist --sign: list game DLLs: %w", err)
	}
	return append(files, dlls...), nil
}
