//go:build linux

package worker

import "syscall"

func setParentDeath(attr *syscall.SysProcAttr) { attr.Pdeathsig = syscall.SIGKILL }
