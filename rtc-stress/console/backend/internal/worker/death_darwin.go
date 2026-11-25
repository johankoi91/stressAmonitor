//go:build darwin

package worker

import "syscall"

func setParentDeath(attr *syscall.SysProcAttr) {}
