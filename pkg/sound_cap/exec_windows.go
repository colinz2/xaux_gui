//go:build windows

package sound_cap

import (
	"golang.org/x/sys/windows"
	"syscall"
)

var procAttrs = &syscall.SysProcAttr{
	NoInheritHandles: false,
	CreationFlags:    windows.CREATE_NO_WINDOW,
}
