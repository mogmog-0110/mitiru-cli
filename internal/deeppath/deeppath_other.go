//go:build !windows

package deeppath

// ntPathUTF8Len は Windows 以外では 0 (NVIDIA のカーネルドライバーの問題は Windows だけ)。
func ntPathUTF8Len(string) int { return 0 }
