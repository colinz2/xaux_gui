package ffaudio

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

import "C"

func DevInfoFormat(p unsafe.Pointer) string {
	return windows.UTF16PtrToString((*uint16)(p))
}
