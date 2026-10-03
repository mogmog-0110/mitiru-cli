package hunt

// mtrr.go ── explorer=from-recording:<path> (N2) 用の .mtrr → input-script 逆変換。
// ファイルの読み方は mtrr_frames.go。
//
// payload (module::InputSnapshot) の先頭 768B は ABI バージョンに関わらず固定
// (keysDown[256] keysJustPressed[256] keysJustReleased[256]。新フィールドは末尾に追記する
// 規約、ModuleApi.hpp 参照)。in-process 注入で使う「押した/離した」だけを読めば足りるので
// それ以降 (マウス・gamepad 等) は解析しない。

import "fmt"

const (
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
// frameSize < 768 (旧すぎる録画) はエラーを返す。
func ExtractEventsFromRecording(path string) ([]Event, error) {
	m, err := openMtrr(path)
	if err != nil {
		return nil, fmt.Errorf("hunt: %w", err)
	}
	defer m.Close()
	if m.frameSize < keysArrayEnd {
		return nil, fmt.Errorf("hunt: recording %s: frameSize=%d が古すぎて keys 配列が読めません", path, m.frameSize)
	}

	var events []Event
	for {
		frame, ok, err := m.next()
		if err != nil {
			return nil, fmt.Errorf("hunt: %w", err)
		}
		if !ok {
			return events, nil
		}
		events = appendKeyEvents(events, frame)
	}
}

func appendKeyEvents(events []Event, frame mtrrFrame) []Event {
	justPressed := frame.input[keysJustPressedOff:keysJustReleasedOff]
	justReleased := frame.input[keysJustReleasedOff:keysArrayEnd]
	for vk := 0; vk < 256; vk++ {
		if justPressed[vk] != 0 {
			events = append(events, fmt.Sprintf("%d %s down", frame.index, keyName(byte(vk))))
		}
		if justReleased[vk] != 0 {
			events = append(events, fmt.Sprintf("%d %s up", frame.index, keyName(byte(vk))))
		}
	}
	return events
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
