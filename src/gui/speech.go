package gui

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"xaux_gui/pkg/sound_cap"

	"github.com/realzhangm/xaux/pkg/x"
	"github.com/sqweek/dialog"
)

var audioFileID atomic.Uint64

type asrResultTarget interface {
	updateASRResult(capture *asrSoundCap, response *x.AllResponse)
}

type asrSoundCap struct {
	deviceName     string
	soundCap       *sound_cap.SoundCap
	resultIndex    int
	writeListIndex int
	isMicrophone   bool
	target         asrResultTarget
}

func (a *asrSoundCap) onResult(response *x.AllResponse) error {
	if a.target != nil {
		a.target.updateASRResult(a, response)
	}
	return nil
}

func (a *asrSoundCap) Run() error {
	return a.soundCap.Run()
}

func (a *asrSoundCap) Stop() {
	a.soundCap.Close()
	a.soundCap.DumpRecordAudio()
}

func newASRSoundCap(
	ctx context.Context,
	proxyAddr, deviceName, savePath string,
	isMicrophone bool,
) (*asrSoundCap, error) {
	mode := sound_cap.CaptureSpeaker
	sourceName := "speaker"
	if isMicrophone {
		mode = sound_cap.CaptureMicrophone
		sourceName = "microphone"
	}
	result := &asrSoundCap{
		deviceName:     deviceName,
		resultIndex:    -1,
		writeListIndex: -1,
		isMicrophone:   isMicrophone,
	}
	capture, err := sound_cap.NewSoundCap(ctx, &sound_cap.Config{
		ProxyAddr:      proxyAddr,
		DeviceName:     deviceName,
		Mode:           mode,
		RecordFilePath: savePath,
	}, result.onResult)
	if err != nil {
		return nil, fmt.Errorf("create %s capture: %w", sourceName, err)
	}
	result.soundCap = capture
	return result, nil
}

func audioFilePath(dir string, isMicrophone bool) string {
	sourceName := "speaker"
	if isMicrophone {
		sourceName = "microphone"
	}
	fileName := fmt.Sprintf(
		"%s_%06d_%s.wav",
		time.Now().Format("20060102-150405"),
		audioFileID.Add(1),
		sourceName,
	)
	return filepath.Join(dir, fileName)
}

func newASRSoundCaps(setting *Setting) ([]*asrSoundCap, error) {
	captures := make([]*asrSoundCap, 0, 2)
	if setting.microphoneSelected {
		capture, err := newASRSoundCap(
			context.Background(), setting.proxyAddr, setting.microphoneName,
			audioFilePath(setting.audioSaveDir, true), true,
		)
		if err != nil {
			return nil, err
		}
		captures = append(captures, capture)
	}
	if setting.speakerSelected {
		capture, err := newASRSoundCap(
			context.Background(), setting.proxyAddr, setting.speakerName,
			audioFilePath(setting.audioSaveDir, false), false,
		)
		if err != nil {
			stopASRSoundCaps(captures)
			return nil, err
		}
		captures = append(captures, capture)
	}
	return captures, nil
}

func stopASRSoundCaps(captures []*asrSoundCap) {
	for _, capture := range captures {
		capture.Stop()
	}
}

func runRealTimeTrans() {
	defer func() {
		if info := recover(); info != nil {
			dialog.Message("%v", info).Title("打开错误").Error()
		}
	}()

	setting, err := currentAppSetting()
	if err != nil {
		panic(err)
	}
	window := newSpeechRTTrans()
	if window == nil {
		panic("实时转写窗口已经打开")
	}

	var captures []*asrSoundCap
	windowStarted := false
	defer func() {
		if !windowStarted {
			stopASRSoundCaps(captures)
			window.release()
		}
	}()
	captures, err = newASRSoundCaps(&setting.Setting)
	if err != nil {
		panic(err)
	}
	for _, capture := range captures {
		capture.target = window
		go func(capture *asrSoundCap) {
			_ = capture.Run()
		}(capture)
		if err := <-capture.soundCap.StartDone(); err != nil {
			panic(fmt.Errorf("start %s: %w", capture.deviceName, err))
		}
		time.Sleep(time.Second)
	}

	window.start(
		func() { stopASRSoundCaps(captures) },
		func() {
			for _, capture := range captures {
				capture.soundCap.Pause()
			}
		},
		func() {
			for _, capture := range captures {
				_ = capture.soundCap.Resume()
			}
		},
	)
	windowStarted = true
}
