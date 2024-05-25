package ffaudio

/*
#cgo windows CFLAGS: -I . -DFFAUDIO_INTERFACE="ffwasapi"
#cgo windows LDFLAGS: -lole32
*/
import "C"

/*
#cgo windows CFLAGS: -I . -DFFAUDIO_INTERFACE="ffwasapi"
#cgo windows LDFLAGS: -lole32
#cgo windows CFLAGS: -I . -DFFAUDIO_INTERFACE="ffdsound"
#cgo windows LDFLAGS: -ldsound -ldxguid
*/
