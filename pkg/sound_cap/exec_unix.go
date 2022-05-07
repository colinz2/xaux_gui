//go:build !windows
// +build !windows

// ref : https://github.com/codemodify/systemkit-processes/tree/master/internal
package internal

import (
	"golang.org/x/sys/unix"
)

var procAttrs = &unix.SysProcAttr{}
