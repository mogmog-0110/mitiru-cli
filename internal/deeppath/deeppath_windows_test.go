//go:build windows

package deeppath

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck_ShallowPlaceIsFine(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "data", "mitiru_host.exe")
	if err := Check(exe); err != nil {
		t.Fatalf("shallow path must pass: %v", err)
	}
}

// 日本語は UTF-8 で 1 文字 3 バイトなので、130 文字ほどのフォルダ名 1 つで 400 バイトを超える。
// これから作る置き場 (まだ存在しない) でも測れる。
func TestCheck_DeepJapanesePlaceStopsBeforeItExists(t *testing.T) {
	deep := filepath.Join(t.TempDir(), strings.Repeat("深", 130), "data", "mitiru_host.exe")
	err := Check(deep)
	var tooDeep *TooDeepError
	if !errors.As(err, &tooDeep) {
		t.Fatalf("want TooDeepError, got %v", err)
	}
	if tooDeep.Bytes <= LimitUTF8Bytes {
		t.Errorf("bytes = %d, want > %d", tooDeep.Bytes, LimitUTF8Bytes)
	}
	if !strings.Contains(err.Error(), "止めた") || !strings.Contains(err.Error(), `C:\Games`) {
		t.Errorf("the message must say what to do: %q", err.Error())
	}
}

// ASCII だけなら同じ文字数でも 400 バイトに届かない。物差しは文字数でなくバイト数。
func TestCheck_AsciiOfTheSameLengthPasses(t *testing.T) {
	ascii := filepath.Join(t.TempDir(), strings.Repeat("a", 130), "data", "mitiru_host.exe")
	if err := Check(ascii); err != nil {
		t.Fatalf("130 ASCII characters is far from 400 bytes: %v", err)
	}
}

// 存在する手前のフォルダは NT のパスに解決して測る (\Device\HarddiskVolumeN で始まる)。
func TestNtPathOf_StartsWithDevice(t *testing.T) {
	nt := ntPathOf(t.TempDir())
	if !strings.HasPrefix(nt, `\Device\`) {
		t.Fatalf("nt path = %q", nt)
	}
}
