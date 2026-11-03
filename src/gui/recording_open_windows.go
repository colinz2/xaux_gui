//go:build windows

package gui

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func openRecordingFile(path string) error {
	return openWindowsRecordingFile(path, windows.ShellExecute)
}

func openWindowsRecordingFile(path string, shellExecute func(windows.Handle, *uint16, *uint16, *uint16, *uint16, int32) error) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("录音路径无效：%w", err)
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	return shellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}
