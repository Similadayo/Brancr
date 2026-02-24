package executor

type TerminalResponse struct {
	Text       string
	Structured any
	Stop       bool
}
