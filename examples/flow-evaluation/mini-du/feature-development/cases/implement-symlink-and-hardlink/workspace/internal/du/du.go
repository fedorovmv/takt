package du

import (
	"errors"
	"io"
)

var ErrNotImplemented = errors.New("du is not implemented")

func Run(_ []string, _ io.Writer, _ io.Writer) error { return ErrNotImplemented }
