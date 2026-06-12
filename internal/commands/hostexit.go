package commands

import "fmt"

// hostExitHint は host の終了コードを 16 進 NTSTATUS としてデコードし、
// 既知のコードには対処ヒントを付ける。run / watch / verify で共通利用 (R-02)。
// Windows の NTSTATUS は環境により負の int32 で返るため uint32 経由で正規化する。
func hostExitHint(code int) string {
	u := uint32(code)
	hex := fmt.Sprintf("0x%08X", u)
	switch u {
	case 0xC0000135: // STATUS_DLL_NOT_FOUND
		return hex + " (DLL not found — SDL2.dll/libcef.dll が host の隣にあるか、" +
			"Debug ビルドなら Debug CRT (msvcp140d 等) が PATH にあるか確認。`mitiru doctor` で診断可)"
	case 0xC0000005: // STATUS_ACCESS_VIOLATION
		return hex + " (access violation — host がクラッシュ。ゲーム DLL の null 参照等を確認)"
	case 0xC0000409: // STATUS_STACK_BUFFER_OVERRUN / fail-fast
		return hex + " (stack buffer overrun / fail-fast)"
	}
	return hex
}

// hostExitReason は host 即死時の exit code を人間語に変換する (verify の A6)。
func hostExitReason(code int) string {
	return fmt.Sprintf("host exited immediately (exit code %s)", hostExitHint(code))
}
