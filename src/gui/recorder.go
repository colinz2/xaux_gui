package gui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"xaux_gui/pkg/sound_cap"
	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sqweek/dialog"
)

const kRecordWinName = "录音"

var recorderWindows struct {
	sync.Mutex
	active *RecorderWin
}

// RecorderWin controls two independent recording-only SoundCap sessions.
// Microphone and speaker audio are deliberately saved to separate WAV files.
type RecorderWin struct {
	window  fyne.Window
	sources []*recordSource
	closed  atomic.Bool
}

type recordSource struct {
	parent       *RecorderWin
	title        string
	deviceName   string
	saveDir      string
	mode         sound_cap.CaptureMode
	isMicrophone bool
	button       *widget.Button
	status       *widget.Label
	errorLabel   *widget.Label
	filePath     *widget.Entry
	mu           sync.Mutex
	capture      *sound_cap.SoundCap
	lastErr      error
}

func runRecorder() {
	defer func() {
		if info := recover(); info != nil {
			dialog.Message("%v", info).Title("打开错误").Error()
		}
	}()

	recorderWindows.Lock()
	if active := recorderWindows.active; active != nil {
		active.window.Show()
		active.window.RequestFocus()
		recorderWindows.Unlock()
		return
	}

	app, exist := AppResisterMap()[AppSettingName]
	if !exist {
		recorderWindows.Unlock()
		panic("找不到录音设置")
	}
	setting := app.(*appSetting).Setting
	win := &RecorderWin{}
	win.window = fyne.CurrentApp().NewWindow(kRecordWinName)
	win.sources = []*recordSource{
		newRecordSource(win, "麦克风", setting.microPhoneName, setting.audioSavaDir,
			sound_cap.CaptureMicrophone, true),
		newRecordSource(win, "扬声器（系统声音）", setting.speakerName, setting.audioSavaDir,
			sound_cap.CaptureSpeaker, false),
	}
	win.window.SetContent(win.makeContent())
	win.window.Resize(fyne.NewSize(920, 570))
	win.window.CenterOnScreen()
	win.window.SetCloseIntercept(func() {
		win.closed.Store(true)
		win.stopAllAndWait()
		win.window.Close()
	})
	win.window.SetOnClosed(func() {
		win.closed.Store(true)
		win.stopAllAndWait()
		recorderWindows.Lock()
		if recorderWindows.active == win {
			recorderWindows.active = nil
		}
		recorderWindows.Unlock()
	})
	recorderWindows.active = win
	recorderWindows.Unlock()
	win.window.Show()
}

// stopActiveRecorder is called before ffaudio.UnInit during application exit.
func stopActiveRecorder() {
	recorderWindows.Lock()
	active := recorderWindows.active
	recorderWindows.Unlock()
	if active != nil {
		active.closed.Store(true)
		active.stopAllAndWait()
	}
}

func (w *RecorderWin) makeContent() fyne.CanvasObject {
	intro := widget.NewLabel("两路可独立或同时录音，分别保存为 16 kHz、单声道 WAV，不进行混音。")
	intro.Wrapping = fyne.TextWrapBreak
	saveDir := widget.NewLabel("保存目录：" + w.sources[0].saveDir)
	saveDir.Wrapping = fyne.TextWrapBreak

	startAll := widget.NewButtonWithIcon("开始两路", mytheme.ResBegin, func() {
		for _, source := range w.sources {
			source.start()
		}
	})
	stopAll := widget.NewButtonWithIcon("全部停止", mytheme.ResEnd, func() {
		for _, source := range w.sources {
			source.stop()
		}
	})
	controls := container.NewHBox(startAll, stopAll)

	content := container.NewVBox(
		intro,
		saveDir,
		widget.NewSeparator(),
		w.sources[0].content(),
		widget.NewSeparator(),
		w.sources[1].content(),
		controls,
	)
	return container.NewPadded(container.NewVScroll(content))
}

func (w *RecorderWin) stopAllAndWait() {
	var wg sync.WaitGroup
	for _, source := range w.sources {
		capture := source.currentCapture()
		if capture == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			capture.Close()
		}()
	}
	wg.Wait()
}

func newRecordSource(parent *RecorderWin, title, deviceName, saveDir string,
	mode sound_cap.CaptureMode, isMicrophone bool) *recordSource {
	s := &recordSource{
		parent:       parent,
		title:        title,
		deviceName:   deviceName,
		saveDir:      saveDir,
		mode:         mode,
		isMicrophone: isMicrophone,
		status:       widget.NewLabel("未录音"),
		errorLabel:   widget.NewLabel(""),
		filePath:     widget.NewMultiLineEntry(),
	}
	s.errorLabel.Wrapping = fyne.TextWrapBreak
	s.filePath.Wrapping = fyne.TextWrapBreak
	s.filePath.SetMinRowsVisible(2)
	s.filePath.SetPlaceHolder("开始录音后显示完整文件路径")
	s.filePath.Disable()
	s.button = widget.NewButtonWithIcon("开始", mytheme.ResBegin, s.start)
	return s
}

func (s *recordSource) content() fyne.CanvasObject {
	title := widget.NewLabelWithStyle(s.title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	header := container.NewBorder(nil, nil, title, s.button, s.status)
	deviceName := s.deviceName
	if deviceName == "" {
		deviceName = "系统默认（开始时检测）"
	}
	device := widget.NewLabel("设备：" + deviceName)
	device.Wrapping = fyne.TextWrapBreak
	return container.NewVBox(
		header,
		device,
		widget.NewLabel("保存文件："),
		s.filePath,
		s.errorLabel,
	)
}

func (s *recordSource) start() {
	if s.parent.closed.Load() {
		return
	}
	s.mu.Lock()
	if s.capture != nil {
		s.mu.Unlock()
		return
	}
	s.lastErr = nil
	s.mu.Unlock()

	path := audioFilePath(s.saveDir, s.isMicrophone)
	s.filePath.SetText(path)
	s.errorLabel.SetText("")
	s.status.SetText("正在启动...")
	s.button.Disable()

	var capture *sound_cap.SoundCap
	config := &sound_cap.Config{
		DeviceName:     s.deviceName,
		Mode:           s.mode,
		RecordFilePath: path,
		// No ProxyAddr: direct recording must not require an ASR service.
		OnError: func(err error) {
			s.onError(capture, err)
		},
		OnStateChange: func(state sound_cap.State) {
			s.onStateChange(capture, state)
		},
	}
	capture, err := sound_cap.NewSoundCap(context.Background(), config, nil)
	if err != nil {
		s.status.SetText("录音失败")
		s.errorLabel.SetText("错误：" + err.Error())
		s.button.Enable()
		return
	}

	s.mu.Lock()
	s.capture = capture
	s.mu.Unlock()
	if s.parent.closed.Load() {
		go capture.Close()
		return
	}
	go func() {
		_ = capture.Run()
		s.onRunFinished(capture)
	}()
}

func (s *recordSource) stop() {
	capture := s.currentCapture()
	if capture == nil {
		return
	}
	state := capture.State()
	if state == sound_cap.StateStopped || state == sound_cap.StateError {
		return
	}
	s.status.SetText("正在停止...")
	s.button.Disable()
	go capture.Close()
}

func (s *recordSource) currentCapture() *sound_cap.SoundCap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capture
}

func (s *recordSource) isCurrent(capture *sound_cap.SoundCap) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capture == capture
}

func (s *recordSource) onError(capture *sound_cap.SoundCap, err error) {
	if capture == nil || err == nil {
		return
	}
	s.mu.Lock()
	if s.capture != capture {
		s.mu.Unlock()
		return
	}
	s.lastErr = err
	s.mu.Unlock()

	fyne.Do(func() {
		if !s.parent.closed.Load() && s.isCurrent(capture) {
			s.errorLabel.SetText("错误：" + err.Error())
		}
	})
}

func (s *recordSource) onStateChange(capture *sound_cap.SoundCap, state sound_cap.State) {
	if capture == nil {
		return
	}
	fyne.Do(func() {
		if s.parent.closed.Load() || !s.isCurrent(capture) {
			return
		}
		switch state {
		case sound_cap.StateStarting:
			s.status.SetText("正在启动...")
		case sound_cap.StateRunning:
			s.status.SetText("录音中")
			s.button.SetText("停止")
			s.button.SetIcon(mytheme.ResEnd)
			s.button.Enable()
		case sound_cap.StatePaused:
			s.status.SetText("已暂停")
		case sound_cap.StateResuming:
			s.status.SetText("正在恢复...")
		case sound_cap.StateStopped:
			s.mu.Lock()
			hadError := s.lastErr != nil
			s.mu.Unlock()
			if hadError {
				s.status.SetText("已停止（有错误）")
			} else {
				s.status.SetText("已停止")
			}
			s.button.Disable()
		case sound_cap.StateError:
			s.status.SetText("录音失败")
			s.button.Disable()
		}
	})
}

func (s *recordSource) onRunFinished(capture *sound_cap.SoundCap) {
	fyne.Do(func() {
		s.mu.Lock()
		if s.capture != capture {
			s.mu.Unlock()
			return
		}
		s.capture = nil
		lastErr := s.lastErr
		s.mu.Unlock()
		if s.parent.closed.Load() {
			return
		}

		s.button.SetText("开始")
		s.button.SetIcon(mytheme.ResBegin)
		s.button.Enable()
		switch {
		case capture.State() == sound_cap.StateError:
			s.status.SetText("录音失败")
		case lastErr != nil:
			s.status.SetText("已停止（有错误）")
		default:
			s.status.SetText("已停止")
		}
		if capture.State() == sound_cap.StateError && s.errorLabel.Text == "" {
			s.errorLabel.SetText(fmt.Sprintf("错误：录音结束，状态码 %d", capture.State()))
		}
	})
}
