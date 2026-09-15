package hunt

// explorer.go ── mitiru hunt (N2) の入力列生成器。host / cobra に依存しない純関数群。
// 生成する文字列は host の --input-script 形式 ("<frame> <KEY> <down|up>") の1行そのもの。

import (
	"fmt"
	"math/rand"
)

// Event は --input-script の1行。
type Event = string

// DefaultKeys はゲーム非依存の既定入力語彙 (internal/commands/fuzz.go の fuzzKeys と揃える)。
var DefaultKeys = []string{"Left", "Right", "Down", "Up"}

// GenRandom はキーを掴んでは離す segment 列を生成する (fuzz.go の genInput と同アルゴリズム。
// hunt はキー語彙を差し替え可能にしたいため独立実装として複製している)。
func GenRandom(rng *rand.Rand, keys []string, frames int) []Event {
	if len(keys) == 0 || frames < 8 {
		return nil
	}
	events := make([]Event, 0, frames/8)
	for f := 1; f < frames-5; {
		key := keys[rng.Intn(len(keys))]
		hold := 8 + rng.Intn(63) // 8..70
		up := f + hold
		if up > frames-2 {
			up = frames - 2
		}
		events = append(events, fmt.Sprintf("%d %s down", f, key))
		events = append(events, fmt.Sprintf("%d %s up", up, key))
		f += hold + rng.Intn(13)
	}
	return events
}

// ExtendRandom は既存の入力列 (novelty の seed) の末尾フレームより後ろへ、同じ分布で
// ランダムな segment を追加する。base が空なら GenRandom と同じ。
func ExtendRandom(rng *rand.Rand, keys []string, base []Event, frames int) []Event {
	startFrame := 1
	if last := lastEventFrame(base); last > 0 {
		startFrame = last + 1 + rng.Intn(10)
	}
	out := append([]Event{}, base...)
	for f := startFrame; f < frames-5; {
		key := keys[rng.Intn(len(keys))]
		hold := 8 + rng.Intn(63)
		up := f + hold
		if up > frames-2 {
			up = frames - 2
		}
		out = append(out, fmt.Sprintf("%d %s down", f, key), fmt.Sprintf("%d %s up", up, key))
		f += hold + rng.Intn(13)
	}
	return out
}

func lastEventFrame(events []Event) int {
	max := 0
	for _, e := range events {
		var f int
		if _, err := fmt.Sscanf(e, "%d", &f); err == nil && f > max {
			max = f
		}
	}
	return max
}

// GenAllKeysDown は全キーを frame 1 で同時押しし、終端手前で離す
// (人間の指では届かない「全キー同時押し」)。
func GenAllKeysDown(keys []string, frames int) []Event {
	if frames < 3 {
		return nil
	}
	up := frames - 2
	events := make([]Event, 0, len(keys)*2)
	for _, k := range keys {
		events = append(events, fmt.Sprintf("1 %s down", k))
	}
	for _, k := range keys {
		events = append(events, fmt.Sprintf("%d %s up", up, k))
	}
	return events
}

// GenAlternating は1フレームごとにキーを入れ替える (人間の反応速度を超えた交互入力)。
func GenAlternating(keys []string, frames int) []Event {
	if len(keys) == 0 || frames < 3 {
		return nil
	}
	events := make([]Event, 0, frames*2)
	for f := 1; f < frames-1; f++ {
		key := keys[(f-1)%len(keys)]
		events = append(events, fmt.Sprintf("%d %s down", f, key), fmt.Sprintf("%d %s up", f+1, key))
	}
	return events
}

// GenHold はキー語彙の先頭を holdFrames の間ずっと押し続ける
// (「60 秒間同入力」= fps*60 を holdFrames に渡す想定)。frames を超えないよう切り詰める。
func GenHold(keys []string, frames, holdFrames int) []Event {
	if len(keys) == 0 || frames < 3 {
		return nil
	}
	up := holdFrames
	if up > frames-2 {
		up = frames - 2
	}
	if up < 2 {
		up = 2
	}
	return []Event{
		fmt.Sprintf("1 %s down", keys[0]),
		fmt.Sprintf("%d %s up", up, keys[0]),
	}
}

// GenSaveLoadEveryFrame は毎フレーム save→load キーを叩く (人間には不可能な頻度)。
// saveKey/loadKey はゲームごとの割当が分からないため、CLI から明示指定された時だけ有効
// (推測でキーを決めない)。指定が無ければ nil を返し、呼び出し側はこの戦略をスキップする。
func GenSaveLoadEveryFrame(saveKey, loadKey string, frames int) []Event {
	if saveKey == "" || loadKey == "" || frames < 4 {
		return nil
	}
	events := make([]Event, 0, frames*4)
	for f := 1; f < frames-2; f += 2 {
		events = append(events,
			fmt.Sprintf("%d %s down", f, saveKey), fmt.Sprintf("%d %s up", f, saveKey),
			fmt.Sprintf("%d %s down", f+1, loadKey), fmt.Sprintf("%d %s up", f+1, loadKey),
		)
	}
	return events
}

// GenPairs はキー語彙の全順序対 (a を押してから b を押す) を1本ずつ生成する
// (「行動ペアの組み合わせ」の総当たり)。各要素が1回のプローブ分の入力列。
func GenPairs(keys []string, frames int) [][]Event {
	if len(keys) < 1 || frames < 20 {
		return nil
	}
	const hold = 8
	out := make([][]Event, 0, len(keys)*len(keys))
	for _, a := range keys {
		for _, b := range keys {
			f1 := 2
			f2 := f1 + hold + 2
			up1 := f1 + hold
			up2 := f2 + hold
			if up2 > frames-2 {
				continue // frame 予算が足りない組は生成しない
			}
			out = append(out, []Event{
				fmt.Sprintf("%d %s down", f1, a),
				fmt.Sprintf("%d %s up", up1, a),
				fmt.Sprintf("%d %s down", f2, b),
				fmt.Sprintf("%d %s up", up2, b),
			})
		}
	}
	return out
}
