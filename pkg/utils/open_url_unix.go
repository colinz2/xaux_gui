//go:build !windows
// +build !windows

package utils

import (
	"os/exec"
	"runtime"
	"syscall"
)

func OpenURL(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()

	default:
		cmd := exec.Command("xdg-open", url)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
		}
		return cmd.Run()
	}
}
