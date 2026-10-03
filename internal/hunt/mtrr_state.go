package hunt

// mtrr_state.go ── .mtrr の GameMemory state blob を frame 単位で読み出し、byte 単位で
// 比較するためのヘルパー (P2: `mitiru replay --diff` の残り)。
//
// host (`mitiru_host --state-diff A B`) は「最初に分岐した frame 番号」までしか返さない
// (game の reflect schema を読み込んでいないため、どの field が壊れたかは出せない)。
// host の引数を増やさない制約の下で少しでも手掛かりを増やすため、CLI 側でその frame の
// state blob 本体を両ファイルから読み直し、byte offset の範囲だけ表示する。
//
import "fmt"

// ReadStateAtFrame は .mtrr から「録画された論理 frame index」が frameIdx と一致する
// frame の state blob を読み出す。見つからなければ error を返す。
func ReadStateAtFrame(path string, frameIdx uint32) ([]byte, error) {
	m, err := openMtrr(path)
	if err != nil {
		return nil, err
	}
	defer m.Close()
	for {
		frame, ok, err := m.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("mtrr: %s: frame %d not found (%d frames in file)", path, frameIdx, m.frameCount)
		}
		if frame.index == frameIdx {
			return append([]byte{}, frame.state...), nil
		}
	}
}

// ByteRange は差分が見つかった連続 byte 区間 ([Start, End) 半開区間)。
type ByteRange struct {
	Start uint32
	End   uint32
}

// DiffByteRanges は a/b (同じ長さのはず) を byte 比較し、値が異なる連続区間の一覧を返す。
// 長さが違う場合はその旨を呼び出し側が別途扱う (この関数は共通長までしか見ない)。
func DiffByteRanges(a, b []byte) []ByteRange {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var ranges []ByteRange
	inRun := false
	var start int
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			if !inRun {
				inRun = true
				start = i
			}
			continue
		}
		if inRun {
			ranges = append(ranges, ByteRange{Start: uint32(start), End: uint32(i)})
			inRun = false
		}
	}
	if inRun {
		ranges = append(ranges, ByteRange{Start: uint32(start), End: uint32(n)})
	}
	return ranges
}
