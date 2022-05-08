//go:build windows

package ffaudio

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

/*
#include "ffaudio/wasapi.c"
*/
import "C"

func DevIDFormat(p unsafe.Pointer) string {
	return windows.UTF16PtrToString((*uint16)(p))
}
