package device

import "errors"

var (
	ErrNotFound     = errors.New("device not found")
	ErrNameTaken    = errors.New("device name already exists in this org")
	ErrLimitReached = errors.New("org device limit reached")
)

type InvalidInputError struct {
	Message string
}

func (e *InvalidInputError) Error() string {
	return e.Message
}
