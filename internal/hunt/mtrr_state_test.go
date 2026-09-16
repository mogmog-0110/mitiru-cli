package hunt

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// writeTestMtrr は v5 .mtrr を frames 個の frame (frameIdx=0..len-1、指定 state blob 付き) で
// 合成する。frameSize は keysArrayEnd 固定 (中身は使わないのでゼロ埋めでよい)。
func writeTestMtrr(t *testing.T, states [][]byte) string {
	t.Helper()
	const frameSize = keysArrayEnd

	var buf bytes.Buffer
	buf.WriteString("MTRR")
	writeU32 := func(v uint32) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	writeU64 := func(v uint64) { _ = binary.Write(&buf, binary.LittleEndian, v) }

	writeU32(mtrrFormatV5)
	writeU32(frameSize)
	writeU32(uint32(len(states)))
	writeU64(0) // rngSeed
	writeU64(0) // recordedAt
	writeU64(0) // abiVersion
	buf.Write(make([]byte, 64)) // envTag

	payload := make([]byte, frameSize)
	for i, state := range states {
		writeU32(uint32(i)) // frameIdx
		buf.Write(payload)
		writeU32(uint32(len(state))) // stateLen
		buf.Write(state)
		writeU32(0) // checksum (未検証)
	}

	path := filepath.Join(t.TempDir(), "test.mtrr")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write test mtrr: %v", err)
	}
	return path
}

func TestReadStateAtFrame(t *testing.T) {
	states := [][]byte{
		{1, 2, 3, 4},
		{5, 6, 7, 8},
		{9, 9, 9, 9},
	}
	path := writeTestMtrr(t, states)

	got, err := ReadStateAtFrame(path, 1)
	if err != nil {
		t.Fatalf("ReadStateAtFrame(1) error: %v", err)
	}
	if !bytes.Equal(got, states[1]) {
		t.Fatalf("ReadStateAtFrame(1) = %v, want %v", got, states[1])
	}
}

func TestReadStateAtFrameNotFound(t *testing.T) {
	path := writeTestMtrr(t, [][]byte{{1, 2, 3}})
	if _, err := ReadStateAtFrame(path, 99); err == nil {
		t.Fatal("ReadStateAtFrame(99): expected error, got nil")
	}
}

func TestDiffByteRangesNoDiff(t *testing.T) {
	a := []byte{1, 2, 3, 4}
	b := []byte{1, 2, 3, 4}
	if ranges := DiffByteRanges(a, b); len(ranges) != 0 {
		t.Fatalf("DiffByteRanges(equal) = %v, want empty", ranges)
	}
}

func TestDiffByteRangesSingleRun(t *testing.T) {
	a := []byte{1, 2, 3, 4, 5}
	b := []byte{1, 9, 9, 4, 5}
	ranges := DiffByteRanges(a, b)
	want := []ByteRange{{Start: 1, End: 3}}
	if len(ranges) != len(want) || ranges[0] != want[0] {
		t.Fatalf("DiffByteRanges() = %v, want %v", ranges, want)
	}
}

func TestDiffByteRangesMultipleRuns(t *testing.T) {
	a := []byte{1, 2, 3, 4, 5, 6, 7}
	b := []byte{9, 2, 3, 9, 9, 6, 9}
	ranges := DiffByteRanges(a, b)
	want := []ByteRange{{Start: 0, End: 1}, {Start: 3, End: 5}, {Start: 6, End: 7}}
	if len(ranges) != len(want) {
		t.Fatalf("DiffByteRanges() = %v, want %v", ranges, want)
	}
	for i := range want {
		if ranges[i] != want[i] {
			t.Fatalf("DiffByteRanges()[%d] = %v, want %v", i, ranges[i], want[i])
		}
	}
}
