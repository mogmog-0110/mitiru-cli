//go:build windows

package deeppath

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// GetFinalPathNameByHandle に渡す VOLUME_NAME_NT (x/sys/windows に定数が無い)
const volumeNameNT = 0x2

// ntPathUTF8Len は、ドライバーが見る \Device\HarddiskVolumeN\... の形の UTF-8 バイト数を返す。
// junction や subst は本当の置き場に解決した形で測る。
func ntPathUTF8Len(path string) int {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	existing, rest := nearestExisting(abs)
	nt := ntPathOf(existing)
	if nt == "" {
		// NT のパスを取れなければ、ボリューム名の分を足した DOS パスで測る (engine の DeepPathGuard と同じ)
		nt = `\Device\HarddiskVolume00` + strings.TrimPrefix(existing, filepath.VolumeName(existing))
	}
	return len(nt) + len(rest)
}

// nearestExisting は path の手前で実在する一番深いフォルダと、そこから先の残り (先頭に \ を含む) を返す。
func nearestExisting(path string) (existing, rest string) {
	cur := path
	for {
		if _, err := os.Stat(cur); err == nil {
			return cur, strings.ReplaceAll(strings.TrimPrefix(path, cur), "/", `\`)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return cur, strings.ReplaceAll(strings.TrimPrefix(path, cur), "/", `\`)
		}
		cur = parent
	}
}

// ntPathOf は存在するパスの NT のパスを返す。取れなければ空文字列。
func ntPathOf(path string) string {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	h, err := windows.CreateFile(p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameNT)
	if err != nil {
		return ""
	}
	if int(n) >= len(buf) {
		buf = make([]uint16, n+1)
		if n, err = windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameNT); err != nil {
			return ""
		}
	}
	return windows.UTF16ToString(buf[:n])
}
