package gui

import (
	"context"
	"fmt"
	"path"
	"sync/atomic"
	"time"
	"xaux_gui/pkg/sound_cap"

	"github.com/realzhangm/xaux/pkg/x"
	"github.com/sqweek/dialog"
)

var (
	audioFileID int64 = 1
)

type AsrRTSoundCapWin interface {
	AsrUpdateUI(arc *AsrRTSoundCap, rsp *x.AllResponse)
}

type AsrRTSoundCap struct {
	devName        string
	soundCap       *sound_cap.SoundCap
	resultList     []string
	resultIndex    int
	writeListIndex int
	isMicroPhone   bool
	win            AsrRTSoundCapWin
}

func (a *AsrRTSoundCap) CallBack(rsp *x.AllResponse) error {
	if a.win != nil {
		a.win.AsrUpdateUI(a, rsp)
	}
	return nil
}

func (a *AsrRTSoundCap) Run() error {
	return a.soundCap.Run()
}

func (a *AsrRTSoundCap) Stop() {
	a.soundCap.Close()
	a.soundCap.DumpRecordAudio()
}

func MakeAsrTRSoundCap(ctx context.Context,
	proxyAddr, devName, savaPath string, isMicroPhone bool) *AsrRTSoundCap {
	arc := AsrRTSoundCap{
		devName:      devName,
		isMicroPhone: isMicroPhone,
	}

	mode := sound_cap.CaptureSpeaker
	if isMicroPhone {
		mode = sound_cap.CaptureMicrophone
	}
	var err error
	arc.soundCap, err = sound_cap.NewSoundCap(ctx, &sound_cap.Config{
		ProxyAddr:      proxyAddr,
		DeviceName:     devName,
		Mode:           mode,
		RecordFilePath: savaPath,
	}, arc.CallBack)
	if err != nil {
		panic(err)
	}
	return &arc
}

func audioFilePath(dir string, isMicroPhone bool) string {
	// yyyy-mm-dd-hh-MM-ss_ID_microphone.wav
	timeStr := time.Now().Format("20060102-150405")
	fType := "speaker"
	if isMicroPhone {
		fType = "microphone"
	}
	fileExt := "wav"
	fileName := fmt.Sprintf("%s_%06d_%s.%s", timeStr, atomic.LoadInt64(&audioFileID), fType, fileExt)
	atomic.AddInt64(&audioFileID, 1)
	return path.Join(dir, fileName)
}

func MakeAsrRTSoundCapListBySetting(setting *Setting) []*AsrRTSoundCap {
	var list []*AsrRTSoundCap
	if setting.isMicroPhoneSelected {
		m := MakeAsrTRSoundCap(context.TODO(), setting.proxyAddr, setting.microPhoneName,
			audioFilePath(setting.audioSavaDir, true), true)
		list = append(list, m)
	}

	if setting.isSpeakerSelected {
		s := MakeAsrTRSoundCap(context.TODO(), setting.proxyAddr, setting.speakerName,
			audioFilePath(setting.audioSavaDir, false), false)

		list = append(list, s)
	}
	return list
}

func runRealTimeTrans() {
	defer func() {
		if info := recover(); info != nil {
			dialog.Message("%v", info).Title("打开错误").Error()
		}
	}()

	app, exist := AppResisterMap()[AppSettingName]
	if !exist {
		panic("can't find AppSettingName")
	}
	appSettingImp := app.(*appSetting)
	if appSettingImp == nil {
		panic("appSetting is nil!")
	}
	srtWin := NewSpeechRTTrans()
	if srtWin != nil {
		defer srtWin.Release()
	} else {
		panic("srtWin is nil!")
	}

	scList := MakeAsrRTSoundCapListBySetting(&appSettingImp.Setting)
	for i := range scList {
		scList[i].win = srtWin
		go func() {
			scList[i].Run()
		}()
		if err := <-scList[i].soundCap.StartDone(); err != nil {
			panic(fmt.Sprintf("start %s error:%s", scList[i].devName, err.Error()))
		}
		time.Sleep(time.Second)
	}

	srtWin.Start(func() {
		for i := range scList {
			scList[i].Stop()
		}
	}, func() {
		for i := range scList {
			scList[i].soundCap.Pause()
		}
	}, func() {
		for i := range scList {
			scList[i].soundCap.Resume()
		}
	})
}
