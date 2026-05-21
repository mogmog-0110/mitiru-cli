//go:build windows

package install

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// appendUserPath adds opts.TargetDir to the user's PATH via
// HKCU\Environment\Path. No admin required. Broadcasts WM_SETTINGCHANGE so
// new processes (and newly-opened shells) see the change immediately.
//
// Idempotent: if the dir is already on PATH the registry write is skipped.
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

	// Use EXPAND_SZ to preserve any %SystemRoot% style expansions present in
	// the original PATH. If the original was REG_SZ we'd downgrade compared
	// to OS behaviour — REG_EXPAND_SZ is the universal-safe form.
	if err := k.SetExpandStringValue("Path", next); err != nil {
		return fmt.Errorf("write PATH: %w", err)
	}

	broadcastSettingChange()
	fmt.Fprintln(opts.Stdout, "  ... done (新しい terminal で有効になります)")
	return nil
}

// broadcastSettingChange notifies the shell that environment variables
// changed. Without this, already-running explorer / cmd / powershell don't
// pick up the new PATH until reboot.
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
