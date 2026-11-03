//go:build linux

package ffaudio

/*
// Compile the PulseAudio capture backend into this package.
#include "ffaudio/pulse.c"
*/
import "C"

import "unsafe"

func DevIDFormat(p unsafe.Pointer) string {
	return C.GoString((*C.char)(p))
}
