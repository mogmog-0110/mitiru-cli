package hunt

import (
	"bytes"
	"testing"
)

// writeTestMtrr は v5 .mtrr を frameIdx=0..len-1、指定 state blob 付きで合成する。
func writeTestMtrr(t *testing.T, states [][]byte) string {
	t.Helper()
	frames := make([]testFrame, len(states))
	for i, state := range states {
		frames[i] = testFrame{input: make([]byte, keysArrayEnd), state: state}
	}
	return writeMtrr(t, mtrrFormatV5, frames)
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
