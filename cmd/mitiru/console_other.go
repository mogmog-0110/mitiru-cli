//go:build !windows

package main

// Windows 以外のターミナルは UTF-8 が既定。切り替えるものが無い。
func setConsoleUTF8() func() { return func() {} }
