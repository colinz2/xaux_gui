//go:build windows

package sound_cap

import (
	"syscall"

	"golang.org/x/sys/windows"
)

var procAttrs = &syscall.SysProcAttr{
	NoInheritHandles: false,
	CreationFlags:    windows.CREATE_NO_WINDOW,
}
