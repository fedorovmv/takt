package du

import (
	"fmt"
	"io"
)

func Run(paths []string, out io.Writer, _ io.Writer) error {
	for _, p := range paths {
		fmt.Fprintf(out, "0\t%s\n", p)
	}
	return nil
}
