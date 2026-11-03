package ffaudio

/*
#cgo windows CFLAGS: -I . -DFFAUDIO_INTERFACE="ffwasapi"
#cgo windows LDFLAGS: -lole32
#cgo linux CFLAGS: -I . -DFFAUDIO_INTERFACE="ffpulse"
#cgo linux pkg-config: libpulse
*/
import "C"
