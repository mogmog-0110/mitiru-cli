// vsenv.go — VS toolchain (vcvars64.bat) の PATH を抽出して host 子プロセスへ前置する。
//
// Debug ビルドの mitiru_host.exe は非再頒布の Debug CRT
// (msvcp140d / vcruntime140d / vcruntime140_1d / ucrtbased) に依存し、素の
// PowerShell では PATH 上に無い → loader が解決できず 0xC0000135 で即死する。
// CRT のコピー (debug_nonredist) はライセンス・更新追従の両面で不適なので、
// ビルドに使っている vcvars 環境の PATH を起動時に前置して解決させる。
// Release ビルドでは不要だが、前置しても無害。
package build

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// vsEnvMarker は vcvars 評価 batch の出力から PATH 行を拾うための目印。
const vsEnvMarker = "MITIRU_VSENV_PATH="

var (
	vsPathOnce sync.Once
	vsPathVal  string
	vsPathErr  error
)

// VsToolchainPath は vcvars64.bat を 1 回だけ評価し、その環境の PATH 値を返す
// (プロセス内キャッシュ)。vcvars が見つからない / 評価に失敗した場合は error。
func VsToolchainPath() (string, error) {
	vsPathOnce.Do(func() {
		vcvars, err := FindVcvars64()
		if err != nil {
			vsPathErr = err
			return
		}
		vsPathVal, vsPathErr = evalVcvarsPath(vcvars)
	})
	return vsPathVal, vsPathErr
}

// evalVcvarsPath は vcvars64.bat を `cmd /c` で評価し、設定後の PATH を抽出する。
func evalVcvarsPath(vcvars string) (string, error) {
	script := "@echo off\r\n" +
		"set \"PATH=C:\\Program Files (x86)\\Microsoft Visual Studio\\Installer;%PATH%\"\r\n" +
		fmt.Sprintf("call \"%s\" >NUL 2>&1\r\n", vcvars) +
		"if errorlevel 1 exit /b %errorlevel%\r\n" +
		"echo " + vsEnvMarker + "%PATH%\r\n"

	tmp, err := os.CreateTemp("", "mitiru_vsenv-*.bat")
	if err != nil {
		return "", fmt.Errorf("create vsenv batch: %w", err)
	}
	scriptPath := tmp.Name()
	defer func() { _ = os.Remove(scriptPath) }()
	if _, err := tmp.WriteString(script); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write vsenv batch: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close vsenv batch: %w", err)
	}

	out, err := exec.Command("cmd", "/c", scriptPath).Output()
	if err != nil {
		return "", fmt.Errorf("evaluate vcvars64.bat: %w", err)
	}
	p, ok := parseVsEnvPath(string(out))
	if !ok {
		return "", fmt.Errorf("vcvars64.bat output did not contain PATH marker")
	}
	return p, nil
}

// parseVsEnvPath は batch 出力から vsEnvMarker 付きの PATH 行を探す (純関数)。
func parseVsEnvPath(out string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, vsEnvMarker) {
			p := strings.TrimPrefix(line, vsEnvMarker)
			if p != "" {
				return p, true
			}
		}
	}
	return "", false
}

// HostEnv は host 子プロセス用の環境変数 slice を返す。VS toolchain の PATH を
// 前置することで Debug CRT を loader に解決させる。vcvars が無い環境では
// 黙って素の環境を返す (Release では不要なため起動自体は妨げない)。
func HostEnv() []string {
	p, err := VsToolchainPath()
	if err != nil || p == "" {
		return os.Environ()
	}
	return PrependPath(os.Environ(), p)
}

// PrependPath は env の PATH 変数 (大文字小文字不問) に prefix を前置した
// 新しい slice を返す。元の slice は変更しない。PATH が無ければ追加する。
func PrependPath(env []string, prefix string) []string {
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if !replaced && i > 0 && strings.EqualFold(kv[:i], "PATH") {
			out = append(out, kv[:i+1]+prefix+";"+kv[i+1:])
			replaced = true
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, "PATH="+prefix)
	}
	return out
}

// FindInPathList は ";" 区切りの PATH 値から name (ファイル名) が解決できるかを
// 返す。doctor の Debug CRT チェックで使う。
func FindInPathList(pathList, name string) bool {
	for _, dir := range strings.Split(pathList, ";") {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if st, err := os.Stat(dir + string(os.PathSeparator) + name); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}
