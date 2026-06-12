package commands

import (
	"strings"
	"testing"
)

func TestHostExitHint_DllNotFound(t *testing.T) {
	// 正の int / 負の int32 の両表現で同じデコードになること。
	for _, code := range []int{int(uint32(0xC0000135)), int(int32(-1073741515))} {
		got := hostExitHint(code)
		if !strings.Contains(got, "0xC0000135") || !strings.Contains(got, "SDL2.dll") {
			t.Errorf("hostExitHint(%d) = %q, want DLL-not-found hint", code, got)
		}
	}
}

func TestHostExitHint_KnownAndGeneric(t *testing.T) {
	if got := hostExitHint(int(int32(-1073741819))); !strings.Contains(got, "0xC0000005") ||
		!strings.Contains(got, "access violation") {
		t.Errorf("hostExitHint(0xC0000005) = %q", got)
	}
	// 未知のコードは 16 進表記のみ。
	if got := hostExitHint(7); got != "0x00000007" {
		t.Errorf("hostExitHint(7) = %q, want 0x00000007", got)
	}
}
