package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const distNoticesFile = "THIRD_PARTY_NOTICES.txt"

// distNoticesSeparator はゲームの表記と engine の表記の境目。遊ぶ側がどこから engine 側かを読み分けられるようにする。
var distNoticesSeparator = "\r\n" + strings.Repeat("=", 72) + "\r\n\r\n"

// composeNotices は配布物のトップに置く第三者ライセンス表記を作る。ゲームの表記 (あれば) を先に、
// engine の表記を後に並べる。engine の表記は engine のビルドが実際にリンクしたものだけを書き出して
// dataDir (deploy dir の写し) に置くので、無いまま配るとライセンス違反になる。だから無ければエラーにする。
func composeNotices(projectRoot, dataDir string) ([]byte, error) {
	engine, err := os.ReadFile(filepath.Join(dataDir, distNoticesFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("dist: engine の %s が deploy dir に無い。engine が使うライブラリの"+
			"ライセンス表記を付けずに配ることになるので止める。engine を新しい版へ上げるか、"+
			"engine のビルドが表記を生成できるよう Python を入れてビルドし直す", distNoticesFile)
	}
	if err != nil {
		return nil, fmt.Errorf("dist: read engine %s: %w", distNoticesFile, err)
	}
	game, err := os.ReadFile(filepath.Join(projectRoot, distNoticesFile))
	if errors.Is(err, fs.ErrNotExist) {
		return engine, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dist: read %s: %w", distNoticesFile, err)
	}
	var out bytes.Buffer
	out.Write(game)
	if len(game) > 0 && !bytes.HasSuffix(game, []byte("\n")) {
		out.WriteString("\r\n")
	}
	out.WriteString(distNoticesSeparator)
	out.Write(engine)
	return out.Bytes(), nil
}

// writeBundleNotices は合成した表記を bundle のトップに書き、copyDeploy が data/ に写した engine 側の
// 表記を消す (同じ内容を 2 か所に置かない)。onefile の外にも同じものを置くので中身を返す。
func writeBundleNotices(projectRoot, bundleRoot, dataDir string) ([]byte, error) {
	notices, err := composeNotices(projectRoot, dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, distNoticesFile), notices, 0o644); err != nil {
		return nil, fmt.Errorf("dist: write %s: %w", distNoticesFile, err)
	}
	if err := os.Remove(filepath.Join(dataDir, distNoticesFile)); err != nil {
		return nil, fmt.Errorf("dist: remove data/%s: %w", distNoticesFile, err)
	}
	return notices, nil
}
