//go:build linux

/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package supervise

import (
	osexec "os/exec"
	"syscall"
)

// setupChildProcessAttrs 는 Linux 의 prctl(PR_SET_PDEATHSIG) 를 통해 instance
// manager (parent) 가 죽으면 child (postgres) 도 자동 종료되도록 한다 — orphan
// postgres 가 K8s container 종료 후에도 잔존하는 케이스를 차단.
func setupChildProcessAttrs(cmd *osexec.Cmd, sig syscall.Signal) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: sig,
	}
}
