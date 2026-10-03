// Package console は、成功したときに何も出さないための出力の振り分けを持つ。
// 利用者が手を動かす必要のある知らせは呼び出し側がそのまま書き、進み具合や
// 内部の様子は -v / --verbose か MITIRU_LOG=verbose のときだけ出す。
package console

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
)

var verbose atomic.Bool

func init() {
	verbose.Store(verboseFromEnv(os.Getenv))
}

// verboseFromEnv は engine と同じ MITIRU_LOG=verbose を CLI でも受ける。
// MITIRU_VERBOSE=1 は cmake の生ログを見るための古い指定で、詳しい表示も含む。
func verboseFromEnv(getenv func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv("MITIRU_LOG")), "verbose") ||
		strings.TrimSpace(getenv("MITIRU_VERBOSE")) == "1"
}

// SetVerbose は環境変数で有効になったものを -v の既定値 false で消さない。
func SetVerbose(on bool) {
	if on {
		verbose.Store(true)
	}
}

func Verbose() bool { return verbose.Load() }

// ForceVerbose はテストが詳しさを決め打ちにするためのもの。戻り値で元に戻す。
func ForceVerbose(on bool) (restore func()) {
	prev := verbose.Swap(on)
	return func() { verbose.Store(prev) }
}

// Logger は書き先と詳しさを値で持つ。テストでは書き先を差し替える。
type Logger struct {
	out     io.Writer
	verbose bool
}

func NewLogger(out io.Writer, verbose bool) Logger {
	return Logger{out: out, verbose: verbose}
}

func (l Logger) Writer() io.Writer {
	if !l.verbose || l.out == nil {
		return io.Discard
	}
	return l.out
}

func (l Logger) Printf(format string, args ...any) {
	fmt.Fprintf(l.Writer(), format, args...)
}

// Verbosef は標準エラーへ書く。--json などの標準出力を汚さないため。
func Verbosef(format string, args ...any) {
	NewLogger(os.Stderr, Verbose()).Printf(format, args...)
}

// Fverbosef は書き先を呼び出し側が決めているとき (watch の tee など) に使う。
func Fverbosef(w io.Writer, format string, args ...any) {
	NewLogger(w, Verbose()).Printf(format, args...)
}

func VerboseWriter(w io.Writer) io.Writer {
	return NewLogger(w, Verbose()).Writer()
}

// ChildEnv は起動する mitiru_host に詳しい表示を伝える。--verbose 引数だと
// 古い host が未知の引数として起動を拒むので、無視されるだけの環境変数で渡す。
func ChildEnv(env []string) []string {
	if !Verbose() {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(strings.ToUpper(kv), "MITIRU_LOG=") {
			out = append(out, kv)
		}
	}
	return append(out, "MITIRU_LOG=verbose")
}
