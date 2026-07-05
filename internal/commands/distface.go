package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tc-hib/winres"
)

// distFaceArgs は配布物の顔つきフラグを返す。title は mitiru.toml の project.name、
// icon は data/ へ同梱済みの icon.ico (hasIcon 時のみ)。name が空なら title は付けない
// (空 token が後続フラグを食う事故を避ける)。
func distFaceArgs(name string, hasIcon bool) []string {
	var args []string
	if name != "" {
		args = append(args, "--title", name)
	}
	if hasIcon {
		args = append(args, "--icon", "icon.ico")
	}
	return args
}

// mtargsJoin は host argv token 列を launch.mtargs / .bat 用の 1 行へ join する。
// 空白を含む (または空の) token は "..." で囲む — host 側 readSidecarArgs と
// CreateProcessW 経由の CRT 分割、どちらの quote 解釈とも整合する。
func mtargsJoin(tokens []string) string {
	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if t == "" || strings.ContainsAny(t, " \t") {
			t = "\"" + t + "\""
		}
		quoted = append(quoted, t)
	}
	return strings.Join(quoted, " ")
}

// embedExeIcon は exePath の PE リソースへ icoPath のアイコンを埋め込む
// (explorer に見える exe の顔)。既存リソース (MSVC 既定 manifest 等) は保持したまま
// icon group を追加する。失敗しても配布は成立するので caller は警告に留める。
func embedExeIcon(exePath, icoPath string) error {
	icoF, err := os.Open(icoPath)
	if err != nil {
		return fmt.Errorf("open ico: %w", err)
	}
	defer icoF.Close()
	icon, err := winres.LoadICO(icoF)
	if err != nil {
		return fmt.Errorf("parse ico: %w", err)
	}

	src, err := os.Open(exePath)
	if err != nil {
		return fmt.Errorf("open exe: %w", err)
	}
	// .rsrc の無い exe (Go 製 stub 等) は空 set から開始。
	rs, lerr := winres.LoadFromEXE(src)
	if lerr != nil {
		rs = &winres.ResourceSet{}
	}
	// ID(1) = 最初の icon group → explorer が exe アイコンとして拾う。
	if err := rs.SetIcon(winres.ID(1), icon); err != nil {
		src.Close()
		return fmt.Errorf("set icon: %w", err)
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		src.Close()
		return fmt.Errorf("rewind exe: %w", err)
	}

	tmp := exePath + ".tmp"
	dst, err := os.Create(tmp)
	if err != nil {
		src.Close()
		return fmt.Errorf("create temp exe: %w", err)
	}
	werr := rs.WriteToEXE(dst, src)
	src.Close() // Windows: rename 前に閉じる
	cerr := dst.Close()
	if werr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write exe resources: %w", werr)
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	if err := os.Rename(tmp, exePath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace exe: %w", err)
	}
	return nil
}
