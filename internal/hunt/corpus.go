package hunt

// corpus.go ── novelty 探索器 (N2) の到達済み GameMemory ハッシュ集合と seed pool。
// AFL 流の energy: 新規ハッシュを多く見つけた入力列ほど優先して伸ばす対象に選ばれる。

import (
	"bufio"
	"hash/fnv"
	"math/rand"
	"strings"
	"sync"
)

// StateHash は --state-trace の JSONL 1行 (1 フレーム分の reflect 状態) を正規化して
// FNV-1a 64bit へ潰す。フィールド順は host 側の reflect 出力で安定しているので
// 文字列そのままのハッシュで十分 (JSON 再構築コストを避ける)。
func StateHash(line string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.TrimSpace(line)))
	return h.Sum64()
}

// HashTraceFile は --state-trace が書いた JSONL ファイルを読み、1行ずつのハッシュ列を返す。
func HashTraceFile(r *bufio.Reader) []uint64 {
	var hashes []uint64
	for {
		line, err := r.ReadString('\n')
		if line = strings.TrimSpace(line); line != "" {
			hashes = append(hashes, StateHash(line))
		}
		if err != nil {
			break
		}
	}
	return hashes
}

// Seed は novelty pool の1エントリ: 過去に走らせた入力列と、それが最後に発見した
// 新規状態ハッシュ数 (energy の元)。
type Seed struct {
	Events    []Event
	NewHashes int
}

// Corpus は複数 job から共有される到達済みハッシュ集合と seed pool。並列 hunt job から
// 触られるため mutex で保護する。
type Corpus struct {
	mu    sync.Mutex
	seen  map[uint64]struct{}
	seeds []Seed
}

func NewCorpus() *Corpus {
	return &Corpus{seen: make(map[uint64]struct{})}
}

// Absorb は 1 run で観測したハッシュ列を取り込み、新規に見つかった数を返す。
func (c *Corpus) Absorb(hashes []uint64) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, h := range hashes {
		if _, ok := c.seen[h]; !ok {
			c.seen[h] = struct{}{}
			n++
		}
	}
	return n
}

// AddSeed は実行済みの入力列を新規発見数とともに pool に加える。
func (c *Corpus) AddSeed(events []Event, newHashes int) {
	if len(events) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seeds = append(c.seeds, Seed{Events: append([]Event{}, events...), NewHashes: newHashes})
}

// Pick は energy (新規発見数+1) に比例した重みで seed を1つ選ぶ。pool が空なら nil。
func (c *Corpus) Pick(rng *rand.Rand) []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.seeds) == 0 {
		return nil
	}
	total := 0
	for _, s := range c.seeds {
		total += s.NewHashes + 1
	}
	r := rng.Intn(total)
	for _, s := range c.seeds {
		w := s.NewHashes + 1
		if r < w {
			return append([]Event{}, s.Events...)
		}
		r -= w
	}
	return append([]Event{}, c.seeds[len(c.seeds)-1].Events...)
}

// Reached は現時点で集合に入っている状態ハッシュ数を返す (レポート用)。
func (c *Corpus) Reached() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}
