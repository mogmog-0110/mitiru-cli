package hunt

// mtrr.go ── explorer=from-recording:<path> (N2) 用の .mtrr → input-script 逆変換。
// フォーマットは include/mitiru/replay/Recorder.hpp / Player.hpp と対で保守する:
//
//	header (v5=104B / v4=40B): magic"MTRR"[4] version[u32] frameSize[u32] frameCount[u32]
//	                           rngSeed[u64] recordedAt[u64] abiVersion[u64] envTag[64B, v5のみ]
//	frame: frameIdx[u32] payload[frameSize]B stateLen[u32] state[stateLen]B checksum[u32]
//
// payload (module::InputSnapshot) の先頭 768B は ABI バージョンに関わらず固定
// (keysDown[256] keysJustPressed[256] keysJustReleased[256]。新フィールドは末尾に追記する
// 規約、ModuleApi.hpp 参照)。in-process 注入で使う「押した/離した」だけを読めば足りるので
// それ以降 (マウス・gamepad 等) は解析しない。

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const (
	mtrrHeaderBytesV4   = 40
	mtrrHeaderBytesV5   = 104
	mtrrFormatV4        = 4
	mtrrFormatV5        = 5
	keysJustPressedOff  = 256
	keysJustReleasedOff = 512
	keysArrayEnd        = 768
)

// keyName は仮想キーコード → --input-script が受理する名前 (apps/mitiru_host/main.cpp の
// vkToName の逆に合わせた最小サブセット。表に無いキーは10進の VK 番号にフォールバックする
// ── keyNameToVk 側が std::stoi でも受理するため入力として有効)。
func keyName(vk byte) string {
	switch vk {
	case 0x25:
		return "Left"
	case 0x26:
		return "Up"
	case 0x27:
		return "Right"
	case 0x28:
		return "Down"
	case 0x08:
		return "Back"
	case 0x09:
		return "Tab"
	case 0x11:
		return "Ctrl"
	case 0x12:
		return "Alt"
	case 0x20:
		return "Space"
	case 0x0D:
		return "Enter"
	case 0x1B:
		return "Escape"
	case 0x10:
		return "Shift"
	}
	if (vk >= 'A' && vk <= 'Z') || (vk >= '0' && vk <= '9') {
		return string(rune(vk))
	}
	return fmt.Sprintf("%d", vk)
}

// ExtractEventsFromRecording は .mtrr の各フレームの keysJustPressed/JustReleased を
// input-script 形式のイベント列へ逆変換する (explorer=from-recording の入力になる)。
// state blob は読み飛ばす。frameSize < 768 (旧すぎる録画) はエラーを返す。
func ExtractEventsFromRecording(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("hunt: open recording %s: %w", path, err)
	}
	defer f.Close()
	r := bufio.NewReader(f)

	var headV4 [mtrrHeaderBytesV4]byte
	if _, err := io.ReadFull(r, headV4[:]); err != nil {
		return nil, fmt.Errorf("hunt: recording %s: header too short: %w", path, err)
	}
	if string(headV4[0:4]) != "MTRR" {
		return nil, fmt.Errorf("hunt: recording %s: magic mismatch", path)
	}
	version := binary.LittleEndian.Uint32(headV4[4:8])
	frameSize := binary.LittleEndian.Uint32(headV4[8:12])
	frameCount := binary.LittleEndian.Uint32(headV4[12:16])

	switch version {
	case mtrrFormatV5:
		var rest [mtrrHeaderBytesV5 - mtrrHeaderBytesV4]byte
		if _, err := io.ReadFull(r, rest[:]); err != nil {
			return nil, fmt.Errorf("hunt: recording %s: v5 header truncated: %w", path, err)
		}
	case mtrrFormatV4:
		// v4 はヘッダここまで。
	default:
		return nil, fmt.Errorf("hunt: recording %s: unsupported format version %d", path, version)
	}

	if frameSize < keysArrayEnd {
		return nil, fmt.Errorf("hunt: recording %s: frameSize=%d が古すぎて keys 配列が読めません", path, frameSize)
	}

	var events []Event
	payload := make([]byte, frameSize)
	for i := uint32(0); i < frameCount; i++ {
		var frameIdxBuf [4]byte
		if _, err := io.ReadFull(r, frameIdxBuf[:]); err != nil {
			break // 打ち切り録画 (--max-frames 等) は読めた分だけ使う
		}
		frameIdx := binary.LittleEndian.Uint32(frameIdxBuf[:])

		if _, err := io.ReadFull(r, payload); err != nil {
			break
		}
		justPressed := payload[keysJustPressedOff:keysJustReleasedOff]
		justReleased := payload[keysJustReleasedOff:keysArrayEnd]
		for vk := 0; vk < 256; vk++ {
			if justPressed[vk] != 0 {
				events = append(events, fmt.Sprintf("%d %s down", frameIdx, keyName(byte(vk))))
			}
			if justReleased[vk] != 0 {
				events = append(events, fmt.Sprintf("%d %s up", frameIdx, keyName(byte(vk))))
			}
		}

		var stateLenBuf [4]byte
		if _, err := io.ReadFull(r, stateLenBuf[:]); err != nil {
			break
		}
		stateLen := binary.LittleEndian.Uint32(stateLenBuf[:])
		if stateLen > 0 {
			if _, err := io.CopyN(io.Discard, r, int64(stateLen)); err != nil {
				break
			}
		}
		var checksumBuf [4]byte
		if _, err := io.ReadFull(r, checksumBuf[:]); err != nil {
			break // checksum 自体は検証しない (bit-exact 判定は host --replay-test に任せる)
		}
	}
	return events, nil
}

// BranchFromRecording は録画イベント列を cutFrame までに切り詰める。呼び出し側はその後へ
// 別の生成器 (ExtendRandom 等) でランダムな続きを足して分岐入力を作る。
func BranchFromRecording(recorded []Event, cutFrame int) []Event {
	out := make([]Event, 0, len(recorded))
	for _, e := range recorded {
		var f int
		if _, err := fmt.Sscanf(e, "%d", &f); err == nil && f > cutFrame {
			break
		}
		out = append(out, e)
	}
	return out
}
