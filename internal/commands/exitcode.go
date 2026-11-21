package commands

import "fmt"

// ExitCodeError は、メッセージを足さずに指定の終了コードで終わりたいときに返す。
// host の終了コード (view の 1 = 警告あり、clip の 3 / 4 など) をそのまま伝えるのに使う。
// Message が空なら main は何も書かない (host がすでに理由を出している)。
type ExitCodeError struct {
	Code    int
	Message string
}

func (e *ExitCodeError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("終了コード %d", e.Code)
}
