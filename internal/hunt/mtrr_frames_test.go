package hunt

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// testdata/rewind_v6.mtrr は engine の mitiru_host で examples/rewind を
// testdata/rewind_v6_script.txt の入力で 30 フレーム録画したもの (format v6、state は 60 フレームごと)。
const v6Fixture = "testdata/rewind_v6.mtrr"

type testFrame struct {
	input []byte
	state []byte
	raw   bool // v6 で差分を詰めずに生の InputSnapshot を書く (engine が zstd 無しで書いた時の形)
}

// writeMtrr は frameIdx=0..len-1 の .mtrr を合成する。input は全フレーム同じ長さにする。
func writeMtrr(t *testing.T, version uint32, frames []testFrame) string {
	t.Helper()
	frameSize := uint32(keysArrayEnd)
	if len(frames) > 0 {
		frameSize = uint32(len(frames[0].input))
	}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	defer enc.Close()

	var buf bytes.Buffer
	writeU32 := func(v uint32) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	buf.WriteString("MTRR")
	writeU32(version)
	writeU32(frameSize)
	writeU32(uint32(len(frames)))
	buf.Write(make([]byte, 24+64)) // rngSeed, recordedAt, abiVersion, envTag

	prev := make([]byte, frameSize)
	for i, f := range frames {
		writeU32(uint32(i))
		if version == mtrrFormatV6 {
			payload := f.input
			if !f.raw {
				delta := make([]byte, frameSize)
				for j := range delta {
					delta[j] = f.input[j] ^ prev[j]
				}
				payload = enc.EncodeAll(delta, nil)
			}
			writeU32(uint32(len(payload)))
			buf.Write(payload)
		} else {
			buf.Write(f.input)
		}
		copy(prev, f.input)
		writeU32(uint32(len(f.state)))
		buf.Write(f.state)
		writeU32(frameChecksum(uint32(i), f.input, uint32(len(f.state)), f.state))
	}

	path := filepath.Join(t.TempDir(), "test.mtrr")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write test mtrr: %v", err)
	}
	return path
}

// keyFrame は vk を押した (pressed) / 離した (released) フレームの InputSnapshot 先頭を作る。
func keyFrame(pressed, released []byte) []byte {
	in := make([]byte, keysArrayEnd+64)
	for _, vk := range pressed {
		in[vk] = 1
		in[keysJustPressedOff+int(vk)] = 1
	}
	for _, vk := range released {
		in[keysJustReleasedOff+int(vk)] = 1
	}
	return in
}

func TestExtractEventsFromV6Fixture(t *testing.T) {
	got, err := ExtractEventsFromRecording(v6Fixture)
	if err != nil {
		t.Fatalf("ExtractEventsFromRecording: %v", err)
	}
	want := []Event{"3 Space down", "5 Space up", "8 Left down", "9 A down", "12 Left up", "20 A up"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestReadStateAtFrameV6Fixture(t *testing.T) {
	state, err := ReadStateAtFrame(v6Fixture, 0)
	if err != nil {
		t.Fatalf("ReadStateAtFrame(0): %v", err)
	}
	if len(state) == 0 {
		t.Fatal("frame 0 should carry the GameMemory state")
	}
	if state, err = ReadStateAtFrame(v6Fixture, 1); err != nil || len(state) != 0 {
		t.Fatalf("frame 1 = %d bytes, %v; want no state (recorded every 60 frames)", len(state), err)
	}
}

func TestV6RawAndDeltaFramesChain(t *testing.T) {
	const vkSpace, vkA = 0x20, 'A'
	frames := []testFrame{
		{input: keyFrame(nil, nil)},
		{input: keyFrame([]byte{vkSpace}, nil), raw: true},
		{input: keyFrame([]byte{vkA}, []byte{vkSpace})}, // 直前が生のフレームでも差分の元になる
		{input: keyFrame(nil, []byte{vkA})},
	}
	got, err := ExtractEventsFromRecording(writeMtrr(t, mtrrFormatV6, frames))
	if err != nil {
		t.Fatalf("ExtractEventsFromRecording: %v", err)
	}
	want := []Event{"1 Space down", "2 Space up", "2 A down", "3 A up"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestV5StillReads(t *testing.T) {
	frames := []testFrame{{input: keyFrame([]byte{0x25}, nil)}, {input: keyFrame(nil, []byte{0x25})}}
	got, err := ExtractEventsFromRecording(writeMtrr(t, mtrrFormatV5, frames))
	if err != nil {
		t.Fatalf("ExtractEventsFromRecording: %v", err)
	}
	if want := []Event{"0 Left down", "1 Left up"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestChecksumMismatchIsError(t *testing.T) {
	path := writeMtrr(t, mtrrFormatV6, []testFrame{{input: keyFrame([]byte{0x20}, nil)}})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xFF
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractEventsFromRecording(path); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
}

func TestTruncatedRecordingKeepsReadFrames(t *testing.T) {
	data, err := os.ReadFile(v6Fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cut.mtrr")
	if err := os.WriteFile(path, data[:len(data)-3], 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ExtractEventsFromRecording(path)
	if err != nil {
		t.Fatalf("truncated recording should not be an error: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("events = %v, want the 6 events before the cut", got)
	}
}

func TestUnsupportedVersion(t *testing.T) {
	path := writeMtrr(t, 3, nil)
	if _, err := ExtractEventsFromRecording(path); err == nil || !strings.Contains(err.Error(), "version 3") {
		t.Fatalf("err = %v, want unsupported version", err)
	}
}
