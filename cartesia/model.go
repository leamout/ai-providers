package cartesia

import "fmt"

// Error preserves upstream diagnostics without changing event context correlation.
type Error struct {
	Code       string
	Message    string
	RequestID  string
	ContextID  string
	StatusCode int
}

func (e *Error) Error() string {
	return fmt.Sprintf("cartesia %s: %s (HTTP %d, request %s)", e.Code, e.Message, e.StatusCode, e.RequestID)
}
