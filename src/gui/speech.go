package gui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"xaux_gui/pkg/sound_cap"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
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

	asrResultMu    sync.Mutex
	lastASRResult  asrResultSnapshot
	hasASRResult   bool
	lastASREventAt time.Time
}

type asrResultSnapshot struct {
	typeName string
	index    int
	text     string
	interim  bool
}

func (a *asrSoundCap) acceptASRResult(response *x.AllResponse) (bool, time.Duration) {
	if a == nil || response == nil {
		return false, 0
	}

	a.asrResultMu.Lock()
	defer a.asrResultMu.Unlock()

	now := time.Now()
	interval := time.Duration(0)
	if !a.lastASREventAt.IsZero() {
		interval = now.Sub(a.lastASREventAt)
	}
	a.lastASREventAt = now

	if response.Type == x.TypeSentenceStart {
		a.hasASRResult = false
		return true, interval
	}

	snapshot := asrResultSnapshot{
		typeName: response.Type,
		index:    response.Result.Index,
		text:     response.Result.Result,
		interim:  response.Result.Interim,
	}
	if a.hasASRResult && a.lastASRResult == snapshot {
		return false, interval
	}
	a.lastASRResult = snapshot
	a.hasASRResult = true
	return true, interval
}

func asrResponseIsFinal(response *x.AllResponse) bool {
	return response != nil && response.Type == x.TypeSentenceEnd
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
	onError func(error),
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
		OnError:        onError,
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

func newASRSoundCaps(setting *Setting, session *Session, onError func(error)) ([]*asrSoundCap, error) {
	captures := make([]*asrSoundCap, 0, 2)
	addCapture := func(isMicrophone bool) error {
		deviceName := setting.speakerName
		if isMicrophone {
			deviceName = setting.microphoneName
		}
		path := audioFilePath(session.Directory(), isMicrophone)
		if err := session.AddRecording(filepath.Base(path)); err != nil {
			return err
		}
		capture, err := newASRSoundCap(
			context.Background(), setting.proxyAddr, deviceName, path, isMicrophone, onError,
		)
		if err != nil {
			return err
		}
		captures = append(captures, capture)
		return nil
	}

	if setting.microphoneSelected {
		if err := addCapture(true); err != nil {
			stopASRSoundCaps(captures)
			return nil, err
		}
	}
	if setting.speakerSelected {
		if err := addCapture(false); err != nil {
			stopASRSoundCaps(captures)
			return nil, err
		}
	}
	return captures, nil
}

func stopASRSoundCaps(captures []*asrSoundCap) {
	for _, capture := range captures {
		capture.Stop()
	}
}

func runRealTimeTrans() {
	activeSpeech.Lock()
	transcriber := activeSpeech.window
	activeSpeech.Unlock()
	if transcriber != nil {
		if transcriber.window != nil {
			transcriber.window.Show()
			transcriber.window.RequestFocus()
		}
		return
	}
	if activeTask := activeVoiceTask(); activeTask != "" {
		fyneDialog.ShowInformation(
			"转写",
			"当前正在使用“"+activeTask+"”任务，请先结束后再开始转写。",
			currentMainWin.win,
		)
		return
	}
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("留空则使用当前时间")
	nameEntryContainer := container.NewGridWrap(
		fyne.NewSize(380, nameEntry.MinSize().Height),
		nameEntry,
	)
	nameDialog := fyneDialog.NewForm(
		"新建转写会话",
		"开始",
		"取消",
		[]*widget.FormItem{widget.NewFormItem("会话名称", nameEntryContainer)},
		func(confirmed bool) {
			if confirmed {
				startRealTimeTrans(nameEntry.Text)
			}
		},
		currentMainWin.win,
	)
	nameDialog.Resize(fyne.NewSize(520, nameDialog.MinSize().Height))
	nameDialog.Show()
}

func startRealTimeTrans(requestedName string) {
	var (
		window        *speechRTTrans
		session       *Session
		captures      []*asrSoundCap
		windowStarted bool
	)
	defer func() {
		info := recover()
		if !windowStarted {
			stopASRSoundCaps(captures)
			if session != nil {
				var taskErr error
				if info != nil {
					taskErr = fmt.Errorf("%v", info)
				}
				if err := session.Finish(SessionStatusFailed, nil, taskErr); err != nil {
					log.Printf("finish failed transcription session: %v", err)
				}
			}
			if window != nil {
				window.release()
			}
		}
		if info != nil {
			dialog.Message("%v", info).Title("打开错误").Error()
		}
	}()

	setting, err := currentAppSetting()
	if err != nil {
		panic(err)
	}
	if setting.proxyAddr == "" {
		panic("Proxy 未启动或地址未配置，请先启动 Proxy 后重试")
	}
	window = newSpeechRTTrans()
	if window == nil {
		activeTask := activeVoiceTask()
		fyneDialog.ShowInformation(
			"转写",
			"当前正在使用“"+activeTask+"”任务，请先结束后再开始转写。",
			currentMainWin.win,
		)
		return
	}
	session, err = CreateSession(setting.sessionRootDir, requestedName, SessionKindTranscription)
	if err != nil {
		panic(err)
	}
	captures, err = newASRSoundCaps(&setting.Setting, session, window.setCaptureError)
	if err != nil {
		if errors.Is(err, sound_cap.ErrASRProxyUnavailable) {
			log.Printf("ASR proxy unavailable: %v", err)
			panic(fmt.Sprintf("Proxy 未启动，请先启动 Proxy 后重试（%s）", setting.proxyAddr))
		}
		panic(err)
	}
	for _, capture := range captures {
		capture.target = window
		go func(capture *asrSoundCap) {
			if err := capture.Run(); err != nil {
				window.setCaptureError(err)
			}
		}(capture)
		if err := <-capture.soundCap.StartDone(); err != nil {
			panic(fmt.Errorf("start %s: %w", capture.deviceName, err))
		}
		time.Sleep(time.Second)
	}

	window.sessionStartedAt = session.Snapshot().CreatedAt
	window.start(
		func() {
			stopASRSoundCaps(captures)
			var transcript []SessionTranscriptEntry
			fyne.DoAndWait(func() {
				window.running.Store(false)
				transcript = window.transcriptEntries()
			})
			status := SessionStatusStopped
			runErr := window.runError()
			if runErr != nil {
				status = SessionStatusFailed
			}
			if err := session.Finish(status, transcript, runErr); err != nil {
				log.Printf("finish transcription session: %v", err)
			}
		},
		func() error {
			var pauseErr error
			for _, capture := range captures {
				pauseErr = errors.Join(pauseErr, capture.soundCap.Pause())
			}
			return pauseErr
		},
		func() error {
			var resumeErr error
			for _, capture := range captures {
				resumeErr = errors.Join(resumeErr, capture.soundCap.Resume())
			}
			return resumeErr
		},
	)
	windowStarted = true
}
