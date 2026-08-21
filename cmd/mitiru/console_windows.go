package main

import "syscall"

// メニューや engine の警告は UTF-8 で出る。Windows のコンソールの既定は CP932 で、
// そのままだと日本語が全部化ける。出力コードページだけ UTF-8 へ切り替え、終了時に
// 元へ戻す (同じ窓で続けて動く他のツールの表示を巻き込まないため)。
// mitiru run が起動する mitiru_host も同じ切り替えを自前で行うので、子の出力も揃う。

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleOutputCP = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
)

func setConsoleUTF8() func() {
	prev, _, _ := procGetConsoleOutputCP.Call()
	if prev == 0 {
		// コンソールが無い (パイプ / サービス)。表示の問題は起きないので何もしない。
		return func() {}
	}
	procSetConsoleOutputCP.Call(65001)
	return func() { procSetConsoleOutputCP.Call(prev) }
}
