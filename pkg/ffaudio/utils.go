package ffaudio

/*
#include "ffaudio.h"
*/
import "C"
import (
	"fmt"
	"reflect"
	"unsafe"
)

func cBuffToBytes(cBuff *C.audio_buff_data_t) (b []byte) {
	bh := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	bh.Data = uintptr(unsafe.Pointer(cBuff.data))
	bh.Len = int(cBuff.len)
	bh.Cap = int(cBuff.len)
	return b
}

// 两种设备，发音和收音设备

func ListDevPlayback() ([]DevInfo, error) {
	return ListDev(C.FFAUDIO_DEV_PLAYBACK)
}

func ListDevCapture() ([]DevInfo, error) {
	return ListDev(C.FFAUDIO_DEV_CAPTURE)
}

type DevPlaybackAndCapture struct {
	PlaybackDefault     string
	CaptureDefault      string
	PlayBackDevNameList []string
	CaptureDevNameList  []string
}

// FindIndex @return : index, type
func (d *DevPlaybackAndCapture) FindIndex(devName string) (int, string) {
	for i := range d.CaptureDevNameList {
		if devName == d.CaptureDevNameList[i] {
			return i, DevTypeCapture
		}
	}
	for i := range d.PlayBackDevNameList {
		if devName == d.PlayBackDevNameList[i] {
			return i, DevTypeLoopBack
		}
	}
	return -1, ""
}

func GetDevPlaybackAndCapture() (*DevPlaybackAndCapture, error) {
	dpc := &DevPlaybackAndCapture{}
	devInfoList, err := ListDevCapture()
	if err != nil {
		return nil, err
	}
	for i := range devInfoList {
		dpc.CaptureDevNameList = append(dpc.CaptureDevNameList, devInfoList[i].Name)
		if devInfoList[i].IsDefault {
			dpc.CaptureDefault = devInfoList[i].Name
		}
	}
	if len(dpc.CaptureDefault) == 0 && len(devInfoList) > 0 {
		dpc.CaptureDefault = devInfoList[0].Name
	}

	devInfoList, err = ListDevPlayback()
	if err != nil {
		return nil, err
	}
	for i := range devInfoList {
		dpc.PlayBackDevNameList = append(dpc.PlayBackDevNameList, devInfoList[i].Name)
		if devInfoList[i].IsDefault {
			dpc.PlaybackDefault = devInfoList[i].Name
		}
	}
	if len(dpc.PlaybackDefault) == 0 && len(devInfoList) > 0 {
		dpc.PlaybackDefault = devInfoList[0].Name
	}

	return dpc, nil
}

func ListDev(mode C.ffuint) ([]DevInfo, error) {
	var devs []DevInfo
	var err error
	d := C.audio_dev_alloc(mode)
	if d == nil {
		return devs, ErrFFAudioDev
	}

	for {
		r := C.audio_dev_next(d)
		if r > 0 {
			break
		} else if r < 0 {
			C.audio_dev_free(d)
			var errStr string = C.GoString(C.audio_dev_error(d))
			return nil, fmt.Errorf("%w,%s", ErrFFAudioDev, errStr)
		}
		devID := unsafe.Pointer(C.audio_dev_info_DEV_ID(d))
		devIDWStr := DevIDFormat(devID)
		isDefault := false
		if C.audio_dev_info(d, C.FFAUDIO_DEV_IS_DEFAULT) != nil {
			isDefault = true
		}

		dev := DevInfo{
			Name:       C.GoString(C.audio_dev_info(d, C.FFAUDIO_DEV_NAME)),
			DevIDStr:   devIDWStr,
			Format:     int(C.audio_dev_info_MIX_FORMAT_0(d)),
			SampleRate: int(C.audio_dev_info_MIX_FORMAT_1(d)),
			Channels:   int(C.audio_dev_info_MIX_FORMAT_2(d)),
			IsDefault:  isDefault,
		}
		devs = append(devs, dev)
	}

	C.audio_dev_free(d)
	return devs, err
}
