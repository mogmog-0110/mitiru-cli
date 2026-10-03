package build

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// phaseTimer は configure / build / deploy にかかった時間を測る。-v のときだけ 1 行で出す。
type phaseTimer struct {
	last   time.Time
	phases []string
	now    func() time.Time
}

func newPhaseTimer() *phaseTimer {
	return &phaseTimer{last: time.Now(), now: time.Now}
}

func (t *phaseTimer) mark(name string) {
	cur := t.now()
	t.phases = append(t.phases, fmt.Sprintf("%s %.1fs", name, cur.Sub(t.last).Seconds()))
	t.last = cur
}

func (t *phaseTimer) report(w io.Writer) {
	if !verboseOutput() || len(t.phases) == 0 {
		return
	}
	fmt.Fprintf(w, "time: %s\n", strings.Join(t.phases, " / "))
}

// verboseOutput は -v (MITIRU_LOG=verbose) か、古い MITIRU_VERBOSE=1 のときに true。
func verboseOutput() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("MITIRU_LOG")), "verbose") ||
		os.Getenv("MITIRU_VERBOSE") == "1"
}
