package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// EnvVar は engine の場所を mitiru.toml の pin より優先して決める環境変数。
const EnvVar = "MITIRU_ENGINE_ROOT"

// explicitRoot は --engine-root で明示された場所を覚える。環境変数だけで決まったものと区別するのは、
// 古いターミナルに残った値が黙って engine を切り替えるのを、明示と見なさないため。場所ごと覚えるのは、
// 明示の後に環境変数が別の値へ変わったとき、その値まで明示扱いにしないため。
var explicitRoot string

// UseExplicitRoot は --engine-root の値を環境変数と同じ経路に載せ、明示された印を付ける。
// build / run / dist はこの場所を使い、engine の場所が変わる切り替えも意図したものとして通す。
func UseExplicitRoot(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.Setenv(EnvVar, abs); err != nil {
		return err
	}
	explicitRoot = abs
	return nil
}

// OverrideRoot は環境変数 (または --engine-root) が engine の場所を決めているとき、その絶対パスを返す。
// explicit は --engine-root で明示されたか (環境変数だけなら false)。
func OverrideRoot() (root string, explicit bool, ok bool) {
	v := strings.TrimSpace(os.Getenv(EnvVar))
	if v == "" {
		return "", false, false
	}
	abs, err := filepath.Abs(v)
	if err != nil {
		abs = v
	}
	return abs, explicitRoot != "" && SamePath(abs, explicitRoot), true
}

// ClearExplicitRoot は明示の印を外す。test が状態を戻すために使う。
func ClearExplicitRoot() { explicitRoot = "" }

// SamePath は 2 つの engine の場所が同じかを返す。大文字と小文字を同じに見るのは Windows だけ。
func SamePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
