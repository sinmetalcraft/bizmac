package cli

// exitError は終了コードだけを指定したいときに使うエラー。
// メッセージが空なので、main 側では ExitCode を見て静かに終了する。
type exitError struct {
	code int
}

func (e *exitError) Error() string { return "" }

// ExitCode は err に対応する終了コードを返す。err が nil なら 0。
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(*exitError); ok {
		return e.code
	}
	return 1
}
