package hunt

// mtrr_state.go ── .mtrr の GameMemory state blob を frame 単位で読み出し、byte 単位で
// 比較するためのヘルパー (P2: `mitiru replay --diff` の残り)。
//
// host (`mitiru_host --state-diff A B`) は「最初に分岐した frame 番号」までしか返さない
// (game の reflect schema を読み込んでいないため、どの field が壊れたかは出せない)。
// host の引数を増やさない制約の下で少しでも手掛かりを増やすため、CLI 側でその frame の
// state blob 本体を両ファイルから読み直し、byte offset の範囲だけ表示する。
//
// フォーマットは mtrr.go の header コメントと同じ (Recorder.hpp / Player.hpp と対で保守)。

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// ReadStateAtFrame は .mtrr から「録画された論理 frame index」が frameIdx と一致する
// frame の state blob を読み出す。見つからなければ error を返す。
func ReadStateAtFrame(path string, frameIdx uint32) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mtrr: open %s: %w", path, err)
	}
	defer f.Close()
	r := bufio.NewReader(f)

	var headV4 [mtrrHeaderBytesV4]byte
	if _, err := io.ReadFull(r, headV4[:]); err != nil {
		return nil, fmt.Errorf("mtrr: %s: header too short: %w", path, err)
	}
	if string(headV4[0:4]) != "MTRR" {
		return nil, fmt.Errorf("mtrr: %s: magic mismatch", path)
	}
	version := binary.LittleEndian.Uint32(headV4[4:8])
	frameSize := binary.LittleEndian.Uint32(headV4[8:12])
	frameCount := binary.LittleEndian.Uint32(headV4[12:16])

	switch version {
	case mtrrFormatV5:
		var rest [mtrrHeaderBytesV5 - mtrrHeaderBytesV4]byte
		if _, err := io.ReadFull(r, rest[:]); err != nil {
			return nil, fmt.Errorf("mtrr: %s: v5 header truncated: %w", path, err)
		}
	case mtrrFormatV4:
		// v4 はヘッダここまで。
	default:
		return nil, fmt.Errorf("mtrr: %s: unsupported format version %d", path, version)
	}

	payload := make([]byte, frameSize)
	for i := uint32(0); i < frameCount; i++ {
		var frameIdxBuf [4]byte
		if _, err := io.ReadFull(r, frameIdxBuf[:]); err != nil {
			break // 打ち切り録画は読めた分だけ
		}
		recordedIdx := binary.LittleEndian.Uint32(frameIdxBuf[:])

		if _, err := io.ReadFull(r, payload); err != nil {
			break
		}

		var stateLenBuf [4]byte
		if _, err := io.ReadFull(r, stateLenBuf[:]); err != nil {
			break
		}
		stateLen := binary.LittleEndian.Uint32(stateLenBuf[:])

		if recordedIdx == frameIdx {
			state := make([]byte, stateLen)
			if stateLen > 0 {
				if _, err := io.ReadFull(r, state); err != nil {
					return nil, fmt.Errorf("mtrr: %s: frame %d: state blob truncated: %w", path, frameIdx, err)
				}
			}
			return state, nil
		}

		if stateLen > 0 {
			if _, err := io.CopyN(io.Discard, r, int64(stateLen)); err != nil {
				break
			}
		}
		var checksumBuf [4]byte
		if _, err := io.ReadFull(r, checksumBuf[:]); err != nil {
			break
		}
	}
	return nil, fmt.Errorf("mtrr: %s: frame %d not found (%d frames in file)", path, frameIdx, frameCount)
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
