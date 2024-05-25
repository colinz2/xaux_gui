package my_malgo

import (
	"github.com/gen2brain/malgo"
)

func ListDevice(deviceType malgo.DeviceType) ([]malgo.DeviceInfo, error) {
	context, err := malgo.InitContext([]malgo.Backend{malgo.BackendWasapi,
		malgo.BackendCoreaudio,
	}, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = context.Uninit()
		context.Free()
	}()

	// Playback devices.
	infos, err := context.Devices(deviceType)
	if err != nil {
		return nil, err
	}

	var devs []malgo.DeviceInfo
	for _, info := range infos {
		full, err := context.DeviceInfo(deviceType, info.ID, malgo.Shared)
		if err != nil {
			return nil, err
		}
		devs = append(devs, full)
	}
	return devs, nil
}

func ListDeviceLoopBack() ([]malgo.DeviceInfo, error) {
	return ListDevice(malgo.Loopback)
}

func ListDevicePlayBack() ([]malgo.DeviceInfo, error) {
	return ListDevice(malgo.Playback)
}

func ListDeviceCapture() ([]malgo.DeviceInfo, error) {
	return ListDevice(malgo.Capture)
}
