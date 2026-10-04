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

// HostWatchesAssets は engine の mitiru_host が --watch-assets を受け取る世代かを返す。
// その世代は資産の読み直しを include/mitiru/asset/AssetReload.hpp に持つ。
func HostWatchesAssets(engineRoot string) bool {
	_, err := os.Stat(filepath.Join(engineRoot, "include", "mitiru", "asset", "AssetReload.hpp"))
	return err == nil
}
