package react

import "errors"

// ErrTurnAbortRequested is used as a cancel-cause when the UI requests abort of the in-flight turn
// (e.g. Ctrl+C). The session must remain alive and accept new turns after cancellation.
var ErrTurnAbortRequested = errors.New("turn abort requested")

