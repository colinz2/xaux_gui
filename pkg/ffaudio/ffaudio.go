package ffaudio

/*
#include "ffaudio.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
	"unsafe"
)

const (
	ModePlayBack = C.FFAUDIO_PLAYBACK
	ModeCapture  = C.FFAUDIO_CAPTURE
	ModeLoopBack = C.FFAUDIO_LOOPBACK
)

const (
	FormatInt8    = C.FFAUDIO_F_INT8
	FormatUint8   = C.FFAUDIO_F_UINT8
	FormatInt16   = C.FFAUDIO_F_INT16
	FormatInt24   = C.FFAUDIO_F_INT24
	FormatInt24_4 = C.FFAUDIO_F_INT24_4
	FormatInt32   = C.FFAUDIO_F_INT32
	FormatFloat32 = C.FFAUDIO_F_FLOAT32
	FormatFloat64 = C.FFAUDIO_F_FLOAT64
)

var (
	ErrFFAudio            = errors.New("ffaudio error")
	ErrFFAudioDev         = fmt.Errorf("%w: device error", ErrFFAudio)
	ErrFFAudioDevNotFound = fmt.Errorf("%w: device not found", ErrFFAudio)
	ErrFFAudioNotOpen     = fmt.Errorf("%w: stream is not open", ErrFFAudio)
	ErrFFAudioClosed      = fmt.Errorf("%w: stream is closed", ErrFFAudio)
)

type DevInfo struct {
	Name       string
	DevIDStr   string
	Format     int
	SampleRate int
	Channels   int
	IsDefault  bool
}

type Config struct {
	AppName          string
	DeviceName       string // Empty means the system default device.
	BufferLengthMsec int
	SampleRate       int // Zero selects the device mix/default format when available.
	Format           int
	Channels         int
}

// FFAudio owns one capture stream. Writer receives copied native-format PCM;
// use PCMConverter when a different sample rate, layout or encoding is needed.
type FFAudio struct {
	SampleRate       int
	Format           int
	Channels         int
	BufferLengthMsec int

	Writer  io.Writer
	OnError func(error) // Non-fatal notifications, such as capture overruns.

	mu      sync.Mutex
	dev     *C.ffaudio_dev
	devBuf  *C.ffaudio_buf
	opened  bool
	closed  bool
	paused  bool
	started bool
}

var initState struct {
	sync.Mutex
	initialized bool
}

func Init() error {
	initState.Lock()
	defer initState.Unlock()
	if initState.initialized {
		return nil
	}
	conf := &C.ffaudio_init_conf{}
	if ret := C.audio_init(conf); ret != 0 {
		message := ""
		if conf.error != nil {
			message = C.GoString(conf.error)
		}
		return fmt.Errorf("%w: init: code %d: %s", ErrFFAudio, int(ret), message)
	}
	initState.initialized = true
	return nil
}

func UnInit() {
	initState.Lock()
	defer initState.Unlock()
	if !initState.initialized {
		return
	}
	C.audio_uninit()
	initState.initialized = false
}

// findDevID selects a device and copies its mix format into cConf. The device
// iterator must remain alive until audio_open has consumed the device ID.
func findDevID(dev *C.ffaudio_dev, deviceName string, mode int32, cConf *C.ffaudio_conf) (bool, error) {
	if dev == nil {
		return false, ErrFFAudioDev
	}
	for {
		r := C.audio_dev_next(dev)
		if r > 0 {
			return false, nil
		}
		if r < 0 {
			message := C.GoString(C.audio_dev_error(dev))
			return false, fmt.Errorf("%w: enumerate: %s", ErrFFAudioDev, message)
		}
		devName := C.audio_dev_info(dev, C.FFAUDIO_DEV_NAME)
		if C.GoString(devName) != deviceName {
			continue
		}

		deviceID := C.audio_dev_info_DEV_ID(dev)
		if mode == ModeLoopBack {
			deviceID = C.audio_dev_info_LOOPBACK_ID(dev)
		}
		if deviceID == nil {
			return false, fmt.Errorf("%w: device %q has no capture source", ErrFFAudioDev, deviceName)
		}
		cConf.device_id = (*C.char)(deviceID)
		if format := C.audio_dev_info_MIX_FORMAT_0(dev); format > 0 {
			cConf.format = format
		}
		if sampleRate := C.audio_dev_info_MIX_FORMAT_1(dev); sampleRate > 0 {
			cConf.sample_rate = sampleRate
		}
		if channels := C.audio_dev_info_MIX_FORMAT_2(dev); channels > 0 {
			cConf.channels = channels
		}
		return true, nil
	}
}

func NewFFAudio() *FFAudio {
	return &FFAudio{}
}

func (f *FFAudio) allocCAudioConf(conf *Config, mode int32) (*C.ffaudio_conf, error) {
	cConf := C.ffaudio_conf_alloc()
	if cConf == nil {
		return nil, fmt.Errorf("%w: allocate config", ErrFFAudio)
	}

	if conf.DeviceName != "" {
		devMode := C.ffuint(C.FFAUDIO_DEV_PLAYBACK)
		if mode == ModeCapture {
			devMode = C.FFAUDIO_DEV_CAPTURE
		}
		f.dev = C.audio_dev_alloc(devMode)
		if f.dev == nil {
			C.ffaudio_conf_free(cConf)
			return nil, ErrFFAudioDev
		}
		found, err := findDevID(f.dev, conf.DeviceName, mode, cConf)
		if err != nil {
			f.releaseCAudioConf(cConf)
			return nil, err
		}
		if !found {
			f.releaseCAudioConf(cConf)
			return nil, fmt.Errorf("%w: %s", ErrFFAudioDevNotFound, conf.DeviceName)
		}
	}

	if conf.SampleRate > 0 {
		cConf.sample_rate = C.ffuint(conf.SampleRate)
	}
	if conf.Format > 0 {
		cConf.format = C.ffuint(conf.Format)
	}
	if conf.Channels > 0 {
		cConf.channels = C.ffuint(conf.Channels)
	}
	if conf.BufferLengthMsec > 0 {
		cConf.buffer_length_msec = C.ffuint(conf.BufferLengthMsec)
	}
	if conf.AppName != "" {
		cConf.app_name = C.CString(conf.AppName)
	}

	f.updateFormat(cConf)
	return cConf, nil
}

func (f *FFAudio) releaseCAudioConf(conf *C.ffaudio_conf) {
	if conf == nil {
		return
	}
	if conf.app_name != nil {
		C.free(unsafe.Pointer(conf.app_name))
		conf.app_name = nil
	}
	conf.device_id = nil
	if f.dev != nil {
		C.audio_dev_free(f.dev)
		f.dev = nil
	}
	C.ffaudio_conf_free(conf)
}

func (f *FFAudio) updateFormat(conf *C.ffaudio_conf) {
	f.Channels = int(conf.channels)
	f.SampleRate = int(conf.sample_rate)
	f.Format = int(conf.format)
	f.BufferLengthMsec = int(conf.buffer_length_msec)
}

func (f *FFAudio) streamError(op string, code int) error {
	message := ""
	if f.devBuf != nil {
		message = C.GoString(C.audio_error(f.devBuf))
	}
	return fmt.Errorf("%w: %s: code %d: %s", ErrFFAudio, op, code, message)
}

// Open opens a microphone (ModeCapture) or speaker loopback (ModeLoopBack)
// stream. Non-blocking reads allow Stop/Close to interrupt the capture loop.
func (f *FFAudio) Open(conf *Config, mode int32) error {
	if mode != ModeCapture && mode != ModeLoopBack {
		return fmt.Errorf("%w: unsupported capture mode %d", ErrFFAudio, mode)
	}
	if conf == nil {
		conf = &Config{}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ErrFFAudioClosed
	}
	if f.opened {
		return fmt.Errorf("%w: stream is already open", ErrFFAudio)
	}

	cConf, err := f.allocCAudioConf(conf, mode)
	if err != nil {
		return err
	}
	defer f.releaseCAudioConf(cConf)

	f.devBuf = C.audio_alloc()
	if f.devBuf == nil {
		return fmt.Errorf("%w: allocate stream", ErrFFAudio)
	}

	flags := C.ffuint(mode | C.FFAUDIO_O_NONBLOCK)
	ret := C.audio_open(f.devBuf, cConf, flags)
	f.updateFormat(cConf)
	if ret == C.FFAUDIO_EFORMAT {
		// ffaudio returns a supported format in cConf; reopen with it as
		// required by the upstream API (notably for WASAPI loopback).
		ret = C.audio_open(f.devBuf, cConf, flags)
		f.updateFormat(cConf)
	}
	if ret != 0 {
		err := f.streamError("open", int(ret))
		C.audio_free(f.devBuf)
		f.devBuf = nil
		return err
	}
	f.opened = true
	return nil
}

func (f *FFAudio) OpenCapture(conf *Config) error {
	return f.Open(conf, ModeCapture)
}

func (f *FFAudio) OpenLoopBack(conf *Config) error {
	return f.Open(conf, ModeLoopBack)
}

// OpenPlayBack is kept for compatibility. Its historical behaviour is speaker
// loopback capture, not audio playback; new code should call OpenLoopBack.
func (f *FFAudio) OpenPlayBack(conf *Config) error {
	return f.OpenLoopBack(conf)
}

func (f *FFAudio) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ErrFFAudioClosed
	}
	if !f.opened {
		return ErrFFAudioNotOpen
	}
	if f.started || f.paused {
		return nil
	}
	if ret := C.audio_start(f.devBuf); ret != 0 {
		return f.streamError("start", int(ret))
	}
	f.started = true
	return nil
}

func (f *FFAudio) Pause() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || !f.opened || f.paused {
		return nil
	}
	if ret := C.audio_stop(f.devBuf); ret != 0 {
		return f.streamError("pause", int(ret))
	}
	f.paused = true
	f.started = false
	return nil
}

func (f *FFAudio) Resume() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ErrFFAudioClosed
	}
	if !f.opened {
		return ErrFFAudioNotOpen
	}
	if !f.paused {
		return nil
	}
	if ret := C.audio_clear(f.devBuf); ret != 0 {
		return f.streamError("clear paused buffer", int(ret))
	}
	if ret := C.audio_start(f.devBuf); ret != 0 {
		return f.streamError("resume", int(ret))
	}
	f.paused = false
	f.started = true
	return nil
}

func (f *FFAudio) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	if f.dev != nil {
		C.audio_dev_free(f.dev)
		f.dev = nil
	}
	if f.devBuf == nil {
		return nil
	}
	if f.opened {
		// Stop is best-effort here; free remains mandatory after a stop error.
		_ = C.audio_stop(f.devBuf)
	}
	C.audio_free(f.devBuf)
	f.devBuf = nil
	f.opened = false
	f.paused = false
	f.started = false
	return nil
}

// ListenAndRun reads until ctx is cancelled, a fatal device error occurs, or
// Close is called. Every Writer call owns its byte slice; no C buffer is
// retained across audio_read calls.
func (f *FFAudio) ListenAndRun(ctx context.Context) error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return ErrFFAudioClosed
	}
	if !f.opened || f.devBuf == nil {
		f.mu.Unlock()
		return ErrFFAudioNotOpen
	}
	if f.Writer == nil {
		f.mu.Unlock()
		return fmt.Errorf("%w: nil PCM writer", ErrFFAudio)
	}
	f.mu.Unlock()

	if err := f.Start(); err != nil {
		return err
	}

	cBuf := C.audio_buff_data_alloc()
	if cBuf == nil {
		return fmt.Errorf("%w: allocate read buffer", ErrFFAudio)
	}
	defer C.audio_buff_data_free(cBuf)

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		f.mu.Lock()
		if f.closed {
			f.mu.Unlock()
			return nil
		}
		if f.paused {
			f.mu.Unlock()
			continue
		}
		ret := C.audio_read(f.devBuf, cBuf)
		var data []byte
		if ret > 0 {
			data = C.GoBytes(unsafe.Pointer(cBuf.data), C.int(cBuf.len))
		}
		var readErr error
		if ret < 0 && ret != -C.FFAUDIO_ESYNC {
			readErr = f.streamError("read", -int(ret))
		}
		f.mu.Unlock()

		if readErr != nil {
			return readErr
		}
		if ret == -C.FFAUDIO_ESYNC {
			if f.OnError != nil {
				f.OnError(fmt.Errorf("%w: capture overrun", ErrFFAudio))
			}
			continue
		}
		if len(data) == 0 {
			continue
		}
		n, err := f.Writer.Write(data)
		if err != nil {
			return fmt.Errorf("write captured PCM: %w", err)
		}
		if n != len(data) {
			return io.ErrShortWrite
		}
	}
}
