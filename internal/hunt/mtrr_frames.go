package hunt

// mtrr_frames.go ── .mtrr を 1 フレームずつ復元して読む (mtrr.go / mtrr_state.go の共通部)。
// フォーマットは include/mitiru/replay/Recorder.hpp / Player.hpp と対で保守する:
//
//	header (v6/v5=104B / v4=40B): magic"MTRR"[4] version[u32] frameSize[u32] frameCount[u32]
//	                              rngSeed[u64] recordedAt[u64] abiVersion[u64] envTag[64B, v5+]
//	frame v6:    frameIdx[u32] payloadLen[u32] payload[payloadLen]B stateLen[u32] state checksum[u32]
//	frame v5/v4: frameIdx[u32] payload[frameSize]B                  stateLen[u32] state checksum[u32]
//
// v6 の payload は payloadLen == frameSize なら生の InputSnapshot、それ以外は
// zstd(InputSnapshot XOR 直前フレームの InputSnapshot)。最初のフレームの「直前」は全 0。
// checksum は fnv1a-32 over [frameIdx | 復元した InputSnapshot | stateLen | state] なので、
// 照合すれば復号の食い違いも分かる。

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

const (
	mtrrHeaderBytesV4 = 40
	mtrrHeaderBytesV5 = 104
	mtrrFormatV4      = 4
	mtrrFormatV5      = 5
	mtrrFormatV6      = 6

	fnvSeed  uint32 = 2166136261
	fnvPrime uint32 = 16777619
)

// mtrrFrame の input / state は次の next() で上書きされる。持ち越すなら呼び出し側で複製する。
type mtrrFrame struct {
	index uint32
	input []byte
	state []byte
}

type mtrrReader struct {
	path       string
	file       *os.File
	r          *bufio.Reader
	version    uint32
	frameSize  uint32
	frameCount uint32
	read       uint32
	input      []byte
	prev       []byte
	packed     []byte
	state      []byte
	dec        *zstd.Decoder
}

func openMtrr(path string) (*mtrrReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mtrr: open %s: %w", path, err)
	}
	m := &mtrrReader{path: path, file: f, r: bufio.NewReader(f)}
	if err := m.readHeader(); err != nil {
		f.Close()
		return nil, err
	}
	m.input = make([]byte, m.frameSize)
	m.prev = make([]byte, m.frameSize)
	return m, nil
}

func (m *mtrrReader) Close() {
	if m.dec != nil {
		m.dec.Close()
	}
	m.file.Close()
}

func (m *mtrrReader) readHeader() error {
	var head [mtrrHeaderBytesV4]byte
	if _, err := io.ReadFull(m.r, head[:]); err != nil {
		return fmt.Errorf("mtrr: %s: header too short: %w", m.path, err)
	}
	if string(head[0:4]) != "MTRR" {
		return fmt.Errorf("mtrr: %s: magic mismatch", m.path)
	}
	m.version = binary.LittleEndian.Uint32(head[4:8])
	m.frameSize = binary.LittleEndian.Uint32(head[8:12])
	m.frameCount = binary.LittleEndian.Uint32(head[12:16])

	switch m.version {
	case mtrrFormatV6, mtrrFormatV5:
		var rest [mtrrHeaderBytesV5 - mtrrHeaderBytesV4]byte
		if _, err := io.ReadFull(m.r, rest[:]); err != nil {
			return fmt.Errorf("mtrr: %s: v%d header truncated: %w", m.path, m.version, err)
		}
	case mtrrFormatV4:
	default:
		return fmt.Errorf("mtrr: %s: unsupported format version %d", m.path, m.version)
	}
	return nil
}

// next は次のフレームを返す。ok=false は終わり。録画の途中で切れたファイル (--max-frames や
// 異常終了) は読めた分で終わりにし、エラーにしない。復号の失敗と checksum の不一致はエラー。
func (m *mtrrReader) next() (mtrrFrame, bool, error) {
	if m.read >= m.frameCount {
		return mtrrFrame{}, false, nil
	}
	index, ok := m.readU32()
	if !ok {
		return mtrrFrame{}, false, nil
	}
	if ok, err := m.readInput(index); !ok || err != nil {
		return mtrrFrame{}, false, err
	}
	stateLen, ok := m.readU32()
	if !ok {
		return mtrrFrame{}, false, nil
	}
	if uint32(cap(m.state)) < stateLen {
		m.state = make([]byte, stateLen)
	}
	m.state = m.state[:stateLen]
	if _, err := io.ReadFull(m.r, m.state); err != nil {
		return mtrrFrame{}, false, nil
	}
	checksum, ok := m.readU32()
	if !ok {
		return mtrrFrame{}, false, nil
	}
	if want := frameChecksum(index, m.input, stateLen, m.state); checksum != want {
		return mtrrFrame{}, false, fmt.Errorf("mtrr: %s: frame %d: checksum mismatch (file %08x, decoded %08x)",
			m.path, index, checksum, want)
	}
	m.read++
	return mtrrFrame{index: index, input: m.input, state: m.state}, true, nil
}

// readInput は m.input へこのフレームの InputSnapshot を復元する。ok=false は途中で切れている。
func (m *mtrrReader) readInput(index uint32) (bool, error) {
	if m.version != mtrrFormatV6 {
		_, err := io.ReadFull(m.r, m.input)
		return err == nil, nil
	}
	payloadLen, ok := m.readU32()
	if !ok {
		return false, nil
	}
	if payloadLen > m.frameSize {
		return false, fmt.Errorf("mtrr: %s: frame %d: payloadLen %d exceeds frameSize %d",
			m.path, index, payloadLen, m.frameSize)
	}
	if payloadLen == m.frameSize {
		if _, err := io.ReadFull(m.r, m.input); err != nil {
			return false, nil
		}
		copy(m.prev, m.input)
		return true, nil
	}
	if err := m.unpackDelta(index, payloadLen); err != nil {
		return false, err
	}
	return true, nil
}

func (m *mtrrReader) unpackDelta(index, payloadLen uint32) error {
	if uint32(cap(m.packed)) < payloadLen {
		m.packed = make([]byte, payloadLen)
	}
	m.packed = m.packed[:payloadLen]
	if _, err := io.ReadFull(m.r, m.packed); err != nil {
		return fmt.Errorf("mtrr: %s: frame %d: payload truncated: %w", m.path, index, err)
	}
	if m.dec == nil {
		dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return fmt.Errorf("mtrr: zstd decoder: %w", err)
		}
		m.dec = dec
	}
	delta, err := m.dec.DecodeAll(m.packed, m.input[:0])
	if err != nil {
		return fmt.Errorf("mtrr: %s: frame %d: zstd: %w", m.path, index, err)
	}
	if len(delta) != int(m.frameSize) {
		return fmt.Errorf("mtrr: %s: frame %d: decoded %d bytes, want %d", m.path, index, len(delta), m.frameSize)
	}
	for i := range m.input {
		m.input[i] ^= m.prev[i]
	}
	copy(m.prev, m.input)
	return nil
}

func (m *mtrrReader) readU32() (uint32, bool) {
	var buf [4]byte
	if _, err := io.ReadFull(m.r, buf[:]); err != nil {
		return 0, false
	}
	return binary.LittleEndian.Uint32(buf[:]), true
}

func fnvAppend(h uint32, data []byte) uint32 {
	for _, b := range data {
		h ^= uint32(b)
		h *= fnvPrime
	}
	return h
}

func frameChecksum(index uint32, input []byte, stateLen uint32, state []byte) uint32 {
	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], index)
	h := fnvAppend(fnvSeed, u32[:])
	h = fnvAppend(h, input)
	binary.LittleEndian.PutUint32(u32[:], stateLen)
	h = fnvAppend(h, u32[:])
	return fnvAppend(h, state)
}
