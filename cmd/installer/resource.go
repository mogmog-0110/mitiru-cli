//go:build windows

package main

// The installer embeds installer.manifest (asInvoker + longPathAware) via a
// compiled resource object: rsrc_windows_amd64.syso, which the Go toolchain
// links automatically when building this package on windows/amd64.
//
// Why this matters: Windows' installer-detection heuristic auto-flags any
// .exe whose name contains "install"/"setup"/"update"/"patch" as
// requireAdministrator — even when the process only writes to HKCU and
// LOCALAPPDATA (no admin needed). The embedded manifest's asInvoker level
// overrides that heuristic so the double-click flow does not trigger a
// spurious UAC prompt. Without the .syso, MitiruEngine_Installer.exe demands
// admin on launch, breaking the "admin 不要" promise in docs/INSTALLER.md.
//
// Regenerate after editing installer.manifest:
//
//	go generate ./cmd/installer
//
// Requires `rsrc` on PATH (go install github.com/akavel/rsrc@latest).
//
//go:generate rsrc -manifest installer.manifest -arch amd64 -o rsrc_windows_amd64.syso
