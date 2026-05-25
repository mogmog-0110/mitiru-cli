//go:build windows

package install

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// appendUserPath は HKCU\Environment\Path 経由で opts.TargetDir をユーザの
// PATH に追加する。admin 不要。WM_SETTINGCHANGE を broadcast し、新しい
// プロセス (および新規に開いた shell) が変更を即座に認識できるようにする。
//
// 冪等: dir が既に PATH 上にあれば registry の書き込みは skip する。
func appendUserPath(opts Options) error {
	dir := opts.TargetDir
	fmt.Fprintf(opts.Stdout, "  registry: HKCU\\Environment\\Path += %s\n", dir)

	if opts.DryRun {
		fmt.Fprintln(opts.Stdout, "  [dry-run] skipped")
		return nil
	}

	k, _, err := registry.CreateKey(
		registry.CURRENT_USER, `Environment`,
		registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open HKCU\\Environment: %w", err)
	}
	defer k.Close()

	cur, _, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("read PATH: %w", err)
	}

	for _, e := range strings.Split(cur, ";") {
		if strings.EqualFold(strings.TrimSpace(e), dir) {
			fmt.Fprintln(opts.Stdout, "  既に登録済 — skip")
			return nil
		}
	}

	next := cur
	if next != "" && !strings.HasSuffix(next, ";") {
		next += ";"
	}
	next += dir

	// HKCU\Environment 下に保存されるユーザ PATH は、旧来の environment-block
	// 形式により実質 ~2KB 付近で頭打ちになる。これを超えると PATH が黙って
	// 切り詰められ、無関係なツールが壊れる。破壊するより拒否する。
	// (spec: docs/INSTALLER.md, failure mode step 4)
	const maxUserPathLen = 2000
	if len(next) > maxUserPathLen {
		return fmt.Errorf(
			"HKCU\\Environment\\Path が長すぎます (%d 文字, 上限 ~%d) — "+
				"PATH を整理してから再実行するか、--skip-pathenv で進めて手動で %s を PATH に通してください",
			len(next), maxUserPathLen, dir)
	}

	// 元の PATH に含まれる %SystemRoot% 形式の展開を保つため EXPAND_SZ を使う。
	// 元が REG_SZ だった場合でも、OS の挙動と比べて格下げにならない —
	// REG_EXPAND_SZ がどこでも安全な形式。
	if err := k.SetExpandStringValue("Path", next); err != nil {
		return fmt.Errorf("write PATH: %w", err)
	}

	broadcastSettingChange()
	fmt.Fprintln(opts.Stdout, "  ... done (新しい terminal で有効になります)")
	return nil
}

// broadcastSettingChange は environment variable が変わったことを shell に
// 通知する。これがないと、既に起動中の explorer / cmd / powershell は
// reboot まで新しい PATH を認識しない。
func broadcastSettingChange() {
	user32 := syscall.NewLazyDLL("user32.dll")
	send := user32.NewProc("SendMessageTimeoutW")

	const HWND_BROADCAST = uintptr(0xFFFF)
	const WM_SETTINGCHANGE = uintptr(0x001A)
	const SMTO_ABORTIFHUNG = uintptr(0x0002)

	envPtr, _ := syscall.UTF16PtrFromString("Environment")
	var result uintptr
	send.Call(
		HWND_BROADCAST,
		WM_SETTINGCHANGE,
		0,
		uintptr(unsafe.Pointer(envPtr)),
		SMTO_ABORTIFHUNG,
		uintptr(5000),
		uintptr(unsafe.Pointer(&result)),
	)
}
