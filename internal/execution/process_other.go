//go:build !unix

package execution

import "os/exec"

func ConfigureCommand(cmd *exec.Cmd) {}
