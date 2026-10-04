package engine

import (
	"os"
	"path/filepath"
)

// HeadersCleanAtW3 は engine のヘッダが MSVC の /W3 で警告を出さない世代かを返す。
// その世代は環境変数を include/mitiru/core/Env.hpp からだけ読み、std::getenv の
// C4996 を利用者のビルドに出さない。
func HeadersCleanAtW3(engineRoot string) bool {
	_, err := os.Stat(filepath.Join(engineRoot, "include", "mitiru", "core", "Env.hpp"))
	return err == nil
}
