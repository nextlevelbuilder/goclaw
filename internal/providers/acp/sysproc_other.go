//go:build !linux

package acp

import "syscall"

func sysProcAttr() *syscall.SysProcAttr {
	return nil
}
