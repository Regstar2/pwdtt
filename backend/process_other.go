//go:build !windows

package backend

import "os/exec"

func configureBackgroundCommand(cmd *exec.Cmd) {}
