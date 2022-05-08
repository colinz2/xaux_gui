package ffaudio

/*
#include "ffaudio.h"
*/
import "C"
import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"unsafe"
)

const (
	DevTypeLoopBack = "loopback"
	DevTypeCapture  = "capture"
)

const (
	ModePlayBack = C.FFAUDIO_PLAYBACK
	ModeCapture  = C.FFAUDIO_CAPTURE
	ModeLoopBack = C.FFAUDIO_LOOPBACK
)

var (
	ErrFFAudio            = errors.New("ffAudio error")
	ErrFFAudioDev         = fmt.Errorf("%w, dev error", ErrFFAudio)
	ErrFFAudioDevNotFound = fmt.Errorf("%w, dev not found", ErrFFAudio)
)

type Config struct {
	AppName          string
	DeviceName       string
	BufferLengthMsec int
}

type FFAudio struct {
	SampleRate int
	Format     int
	Channels   int

	dev    *C.ffaudio_dev
	devBuf *C.ffaudio_buf
	Writer io.Writer
	Reader io.Reader
}

type DevInfo struct {
	Name       string
	DevIDStr   string
	IsDefault  bool
	Format     int
	SampleRate int
	Channels   int
}

func Init() {
	conf := &C.ffaudio_init_conf{}
	C.audio_init(conf)
}

func UnInit() {
	C.audio_uninit()
}

func findDevID(dev *C.ffaudio_dev, DeviceName string, cConf *C.ffaudio_conf) bool {
	if dev == nil {
		return false
	}
	for {
		r := C.audio_dev_next(dev)
		if r > 0 {
			break
		} else if r < 0 {
			break
		}
		devName := C.audio_dev_info(dev, C.FFAUDIO_DEV_NAME)
		if C.GoString(devName) == DeviceName {
			cConf.device_id = (*C.char)(C.audio_dev_info_DEV_ID(dev))
			cConf.format = C.audio_dev_info_MIX_FORMAT_0(dev)
			cConf.sample_rate = C.audio_dev_info_MIX_FORMAT_1(dev)
			cConf.channels = C.audio_dev_info_MIX_FORMAT_2(dev)
			return true
		}
	}
	return false
}

func NewFFAudio() *FFAudio {
	return &FFAudio{}
}

func (f *FFAudio) allocCAudioConf(conf *Config) *C.ffaudio_conf {
	cConf := C.ffaudio_conf_alloc()
	// 延迟释放 f.dev
	f.dev = C.audio_dev_alloc(C.FFAUDIO_DEV_PLAYBACK)
	if !findDevID(f.dev, conf.DeviceName, cConf) {
		if !findDevID(f.dev, conf.DeviceName, cConf) {
			f.releaseCAudioConf(cConf)
			return nil
		}
	}

	if conf.BufferLengthMsec != 0 {
		cConf.buffer_length_msec = C.ffuint(conf.BufferLengthMsec)
	}

	if len(conf.AppName) != 0 {
		cConf.app_name = C.CString(conf.AppName)
	}

	f.Channels = int(cConf.channels)
	f.SampleRate = int(cConf.sample_rate)
	f.Format = int(cConf.format)
	return cConf
}

func (f *FFAudio) releaseCAudioConf(conf *C.ffaudio_conf) {
	if conf.app_name != nil {
		C.free(unsafe.Pointer(conf.app_name))
		conf.app_name = nil
	}
	if conf.device_id != nil {
		// 释放 f.dev
		C.audio_dev_free(f.dev)
		conf.device_id = nil
	}
	C.ffaudio_conf_free(conf)
}

func (f *FFAudio) open(conf *Config, mode int32) error {
	cConf := f.allocCAudioConf(conf)
	if cConf == nil {
		return ErrFFAudioDevNotFound
	}
	defer f.releaseCAudioConf(cConf)
	ret := C.audio_open(f.devBuf, cConf, C.ffuint(mode))
	if ret != 0 {
		devErrStr := C.GoString((*C.char)(C.audio_error(f.devBuf)))
		return fmt.Errorf("%w, %s, code %d, %s",
			ErrFFAudio, DevIDFormat(unsafe.Pointer(cConf.device_id)), int(ret), string(devErrStr))
	}
	return nil
}

func (f *FFAudio) OpenLoopBack(conf *Config) error {
	f.devBuf = C.audio_alloc()
	// |C.FFAUDIO_O_NONBLOCK
	err := f.open(conf, int32(C.FFAUDIO_LOOPBACK|C.FFAUDIO_O_NONBLOCK))
	if err != nil {
		return err
	}
	return nil
}

func (f *FFAudio) ListenAndRun(ctx context.Context) error {
	ctx1, _ := context.WithCancel(ctx)
	exitChan := make(chan error)
	cBuf := C.audio_buff_data_alloc()
	defer C.audio_buff_data_free(cBuf)
	go func(ctx context.Context) {
		ticker := time.NewTicker(time.Millisecond * 1)
		for {
			select {
			case <-ctx.Done():
				goto _exit
			case <-ticker.C:
				ret := C.audio_read(f.devBuf, cBuf)
				if ret > 0 && f.Writer != nil {
					f.Writer.Write(cBuffToBytes(cBuf))
				}
			}
		}
	_exit:
		fmt.Println("exit!!!")
		ticker.Stop()
		exitChan <- ctx.Err()
	}(ctx1)

	<-exitChan
	return nil
}
