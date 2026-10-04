package main

import "syscall"

// メニューや engine の警告は UTF-8 で出る。Windows のコンソールの既定は CP932 で、
// そのままだと日本語が全部化ける。出力コードページだけ UTF-8 へ切り替え、終了時に
// 元へ戻す (同じ窓で続けて動く他のツールの表示を巻き込まないため)。
// mitiru run が起動する mitiru_host も同じ切り替えを自前で行うので、子の出力も揃う。
// ビルドの batch は chcp で入力コードページも UTF-8 にする (internal/build の
// vcvarsPrelude)。コンソールは子と共有なので、入力の方も終了時に戻す。

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleCP       = kernel32.NewProc("GetConsoleCP")
	procSetConsoleCP       = kernel32.NewProc("SetConsoleCP")
	procGetConsoleOutputCP = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
)

func setConsoleUTF8() func() {
	prevOut, _, _ := procGetConsoleOutputCP.Call()
	if prevOut == 0 {
		// コンソールが無い (パイプ / サービス)。表示の問題は起きないので何もしない。
		return func() {}
	}
	prevIn, _, _ := procGetConsoleCP.Call()
	procSetConsoleOutputCP.Call(65001)
	return func() {
		procSetConsoleOutputCP.Call(prevOut)
		if prevIn != 0 {
			procSetConsoleCP.Call(prevIn)
		}
	}
}
