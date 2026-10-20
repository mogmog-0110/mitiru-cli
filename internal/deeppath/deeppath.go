// Package deeppath は、GPU を使う exe を深すぎるフォルダから起動しないための守り。
//
// NVIDIA のカーネルドライバー (nvlddmkm 32.0.16.1742) は、D3D12 を使う exe の NT パス
// (\Device\HarddiskVolumeN\...) を UTF-8 にして約 512 バイトの領域へ長さを確かめずに写し、
// 溢れるとカーネルのヒープが壊れて PC が落ちる。engine の include/mitiru/platform/DeepPathGuard.hpp が
// host の中で同じ基準 (400 バイト) で止めるが、それは host が起動した後の話なので、
// CLI は起動の前に同じ物差しで測って止まる。
package deeppath

import (
	"fmt"
	"path/filepath"
)

// LimitUTF8Bytes は止める基準。512 バイトに達する前に、ドライバーがパスの末尾へ足す分の余裕を取る。
// engine の kDeepPathLimitUtf8Bytes と同じ値にそろえる。
const LimitUTF8Bytes = 400

// Check は exe を起動してよいかを返す。深すぎるときは、何をすればよいかが分かる短い error を返す。
// exe がまだ無い置き場 (これから作る配布物) でも、手前の存在するフォルダから足して測る。
func Check(exe string) error {
	n := ntPathUTF8Len(exe)
	if n <= LimitUTF8Bytes {
		return nil
	}
	return &TooDeepError{Path: exe, Bytes: n}
}

// TooDeepError は置き場が深すぎるときの error。
type TooDeepError struct {
	Path  string
	Bytes int
}

func (e *TooDeepError) Error() string {
	return fmt.Sprintf("フォルダが深すぎて GPU のドライバーが PC ごと落ちるので止めた。C:\\Games のような浅い所へ移して\n"+
		"  %s の NT のパスが UTF-8 で %d バイト (上限 %d)",
		filepath.Clean(e.Path), e.Bytes, LimitUTF8Bytes)
}
