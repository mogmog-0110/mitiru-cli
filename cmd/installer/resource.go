//go:build windows

package main

// installer は installer.manifest (asInvoker + longPathAware) を
// compiled resource object である rsrc_windows_amd64.syso 経由で埋め込む。
// windows/amd64 でこの package を build すると Go toolchain が自動でリンクする。
//
// なぜ重要か: Windows の installer 検出 heuristic は、
// 名前に "install"/"setup"/"update"/"patch" を含む .exe を
// requireAdministrator として自動 flag する — process が HKCU と
// LOCALAPPDATA にしか書き込まない (admin 不要) 場合でも同様。埋め込んだ
// manifest の asInvoker level がこの heuristic を上書きし、double-click flow が
// 不要な UAC prompt を出さないようにする。.syso が無いと
// MitiruEngine_Installer.exe は起動時に admin を要求し、docs/INSTALLER.md の
// 「admin 不要」 約束を破ってしまう。
//
// installer.manifest を編集したら再生成すること:
//
//	go generate ./cmd/installer
//
// `rsrc` が PATH にあること (go install github.com/akavel/rsrc@latest)。
//
//go:generate rsrc -manifest installer.manifest -arch amd64 -o rsrc_windows_amd64.syso
