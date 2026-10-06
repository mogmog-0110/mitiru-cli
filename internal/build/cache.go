package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// compilerCacheDefines は sccache があるときだけ、configure に渡す cache 変数を返す。
// worktree ごとにエンジンを丸ごと組み直す時間を、同じ翻訳単位の使い回しで削る。
// 見つからない・切られているときは nil で、素の cl と同じ組み方になる。
//
// sccache は共有 PDB (/Zi) のコンパイルを cache できないので、/Z7 (Embedded) へ替える。
// リンク時の PDB は /DEBUG で今までどおり出る。CMP0141 が NEW でないと
// CMAKE_MSVC_DEBUG_INFORMATION_FORMAT が効かないので、全 target に届くよう既定で NEW にする。
//
// setting は mitiru.toml の [build] cache。"none" で切り、空か "sccache" で自動検出する。
// 環境変数 MITIRU_SCCACHE は exe のパスで検出を上書きし、"off" で切る。
func compilerCacheDefines(config, setting string) []string {
	exe := findSccache(setting)
	if exe == "" {
		return nil
	}
	defs := []string{
		"CMAKE_C_COMPILER_LAUNCHER=" + exe,
		"CMAKE_CXX_COMPILER_LAUNCHER=" + exe,
		// PCH (/Yu /Yc /Fp) の cl も sccache は cache しない。Jolt の約 150 本がそこで毎回落ちる
		"CMAKE_DISABLE_PRECOMPILE_HEADERS=ON",
		// Jolt は自前で /Zi を足し、CMake が /Fd も付ける。/Z7 と並ぶと sccache が無い PDB を探して落ちる。
		// 記号は CMAKE_MSVC_DEBUG_INFORMATION_FORMAT の /Z7 が引き継ぐ
		"GENERATE_DEBUG_SYMBOLS=OFF",
	}
	if config == "Debug" || config == "RelWithDebInfo" {
		defs = append(defs,
			"CMAKE_POLICY_DEFAULT_CMP0141=NEW",
			"CMAKE_MSVC_DEBUG_INFORMATION_FORMAT=Embedded")
	}
	return defs
}

func findSccache(setting string) string {
	if strings.EqualFold(setting, "none") {
		return ""
	}
	if env := strings.TrimSpace(os.Getenv("MITIRU_SCCACHE")); env != "" {
		switch strings.ToLower(env) {
		case "off", "0", "none":
			return ""
		}
		if st, err := os.Stat(env); err == nil && !st.IsDir() {
			return filepath.ToSlash(env)
		}
		return ""
	}
	if p, err := exec.LookPath("sccache"); err == nil {
		return filepath.ToSlash(p)
	}
	// winget は PATH の更新が既存のシェルへ届かないことがあるので、置き場所を直に探す。
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return ""
	}
	pattern := filepath.Join(local, "Microsoft", "WinGet", "Packages", "Mozilla.sccache_*", "*", "sccache.exe")
	if m, _ := filepath.Glob(pattern); len(m) > 0 {
		return filepath.ToSlash(m[len(m)-1])
	}
	return ""
}
