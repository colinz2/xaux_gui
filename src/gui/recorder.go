package gui

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"

	"xaux_gui/pkg/sound_cap"
	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sqweek/dialog"
)

const recordWindowTitle = "录音"

var recorderWindows struct {
	sync.Mutex
	active *RecorderWin
}

// RecorderWin owns one shared session for each unified recording start.
// Microphone and speaker audio are saved as separate WAV files in that session.
type RecorderWin struct {
	window        fyne.Window
	sources       []*recordSource
	saveDir       string
	sessionName   *widget.Entry
	durationLabel *widget.Label
	errorLabel    *widget.Label
	startButton   *widget.Button
	stopButton    *widget.Button
	closed        atomic.Bool
	releaseOnce   sync.Once

	mu            sync.Mutex
	session       *Session
	requestedName string
	duration      *sessionDurationView
}

type recordSource struct {
	parent       *RecorderWin
	title        string
	deviceName   string
	mode         sound_cap.CaptureMode
	isMicrophone bool
	selected     bool
	status       *widget.Label
	errorLabel   *widget.Label
	filePath     *widget.Entry

	mu      sync.Mutex
	capture *sound_cap.SoundCap
	lastErr error
}

func runRecorder() {
	var (
		win         *RecorderWin
		taskClaimed bool
	)
	defer func() {
		if info := recover(); info != nil {
			if taskClaimed {
				if win != nil {
					win.closed.Store(true)
					win.stopAllAndWait()
					recorderWindows.Lock()
					if recorderWindows.active == win {
						recorderWindows.active = nil
					}
					recorderWindows.Unlock()
				}
				releaseVoiceTask(recordMenuName)
			}
			dialog.Message("%v", info).Title("打开错误").Error()
		}
	}()

	recorderWindows.Lock()
	if active := recorderWindows.active; active != nil {
		recorderWindows.Unlock()
		active.window.Show()
		active.window.RequestFocus()
		return
	}
	recorderWindows.Unlock()
	if activeTask := activeVoiceTask(); activeTask != "" {
		fyneDialog.ShowInformation(
			"录音",
			"当前正在使用“"+activeTask+"”任务，请先结束后再开始录音。",
			currentMainWin.win,
		)
		return
	}
	if !acquireVoiceTask(recordMenuName) {
		fyneDialog.ShowInformation(
			"录音",
			"当前已有语音任务，请先结束后再开始录音。",
			currentMainWin.win,
		)
		return
	}
	taskClaimed = true

	appSetting, err := currentAppSetting()
	if err != nil {
		panic(err)
	}
	setting := appSetting.Setting
	win = &RecorderWin{
		saveDir:       setting.sessionRootDir,
		sessionName:   widget.NewEntry(),
		durationLabel: widget.NewLabel("时长：--:--:--"),
		errorLabel:    widget.NewLabel(""),
	}
	win.sessionName.SetPlaceHolder("留空则使用当前时间；可自定义本次会话名称")
	win.errorLabel.Wrapping = fyne.TextWrapBreak
	win.setError("")
	win.window = fyne.CurrentApp().NewWindow(recordWindowTitle)
	win.window.SetPadded(false)
	win.sources = []*recordSource{
		newRecordSource(win, "麦克风", setting.microphoneName,
			sound_cap.CaptureMicrophone, true, setting.microphoneSelected),
		newRecordSource(win, "扬声器（系统声音）", setting.speakerName,
			sound_cap.CaptureSpeaker, false, setting.speakerSelected),
	}
	content, windowSize := win.makeContent()
	win.window.SetContent(content)
	win.window.Resize(windowSize)
	win.window.CenterOnScreen()
	win.window.SetCloseIntercept(func() {
		win.closed.Store(true)
		win.stopAllAndWait()
		win.window.Close()
	})
	win.window.SetOnClosed(func() {
		win.closed.Store(true)
		win.stopAllAndWait()
		win.release()
	})
	recorderWindows.Lock()
	recorderWindows.active = win
	recorderWindows.Unlock()
	win.window.Show()
	taskClaimed = false
}

// stopActiveRecorder is called before ffaudio.UnInit during application exit.
func stopActiveRecorder() {
	recorderWindows.Lock()
	active := recorderWindows.active
	recorderWindows.Unlock()
	if active != nil {
		active.closed.Store(true)
		active.stopAllAndWait()
		if active.window != nil {
			fyne.DoAndWait(func() {
				active.window.SetCloseIntercept(nil)
				active.window.Close()
			})
		}
		active.release()
	}
}

func (w *RecorderWin) release() {
	w.releaseOnce.Do(func() {
		recorderWindows.Lock()
		if recorderWindows.active == w {
			recorderWindows.active = nil
		}
		recorderWindows.Unlock()
		releaseVoiceTask(recordMenuName)
	})
}

func (w *RecorderWin) makeContent() (fyne.CanvasObject, fyne.Size) {
	intro := widget.NewLabel("一次开始同时录制已选音源，分别保存为 16 kHz 单声道 WAV。")
	intro.Wrapping = fyne.TextWrapBreak
	saveDir := widget.NewLabel("会话主目录：" + w.saveDir)
	saveDir.Wrapping = fyne.TextWrapBreak

	w.startButton = widget.NewButtonWithIcon("开始录音", mytheme.ResBegin, w.start)
	w.startButton.Importance = widget.HighImportance
	w.stopButton = widget.NewButtonWithIcon("停止录音", mytheme.ResEnd, w.stop)
	w.stopButton.Importance = widget.DangerImportance
	w.stopButton.Disable()
	if len(w.selectedSources()) == 0 {
		w.startButton.Disable()
	}
	buttonSize := fyne.NewSize(190, 56)
	controls := container.NewCenter(container.NewHBox(
		container.NewGridWrap(buttonSize, w.startButton),
		container.NewGridWrap(buttonSize, w.stopButton),
	))

	body := container.NewVBox(
		intro,
		saveDir,
		widget.NewSeparator(),
		container.NewBorder(nil, nil, widget.NewLabel("会话名称"), w.durationLabel, w.sessionName),
		w.errorLabel,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, w.sources[0].content(), w.sources[1].content()),
	)
	footer := container.NewVBox(widget.NewSeparator(), controls)
	size := recorderWindowSize(body, footer, controls)
	content := container.NewPadded(container.NewBorder(
		nil, footer, nil, nil,
		container.NewVScroll(body),
	))
	return content, size
}

func recorderWindowSize(body, footer, controls fyne.CanvasObject) fyne.Size {
	measurement := container.NewPadded(container.NewBorder(nil, footer, nil, nil, body))
	size := measurement.MinSize()
	if size.Width < 960 {
		size.Width = 960
	}
	// Wrapped labels need the target width before their height can be measured.
	body.Resize(fyne.NewSize(size.Width-2*theme.Padding(), body.MinSize().Height))
	size.Height = measurement.MinSize().Height
	visibleGap := theme.Padding() + footer.MinSize().Height - controls.MinSize().Height
	size.Height += fyne.Max(0, theme.TextSize()-visibleGap)
	return size
}

func (w *RecorderWin) selectedSources() []*recordSource {
	var selected []*recordSource
	for _, source := range w.sources {
		if source.selected {
			selected = append(selected, source)
		}
	}
	return selected
}

func (w *RecorderWin) prepareSession(requestedName string) (*Session, map[*recordSource]string, error) {
	session, err := CreateSession(w.saveDir, requestedName, SessionKindRecording)
	if err != nil {
		return nil, nil, err
	}
	paths := make(map[*recordSource]string)
	for _, source := range w.selectedSources() {
		path := audioFilePath(session.Directory(), source.isMicrophone)
		if err := session.AddRecording(filepath.Base(path)); err != nil {
			_ = session.Finish(SessionStatusFailed, nil, err)
			return nil, nil, err
		}
		paths[source] = path
	}
	return session, paths, nil
}

func (w *RecorderWin) start() {
	if w.closed.Load() {
		return
	}
	w.mu.Lock()
	active := w.session != nil
	w.mu.Unlock()
	if active || len(w.selectedSources()) == 0 {
		return
	}

	requestedName := w.sessionName.Text
	session, paths, err := w.prepareSession(requestedName)
	if err != nil {
		w.setError("错误：" + err.Error())
		return
	}
	info := session.Snapshot()
	duration := newSessionDurationView(w.durationLabel, info.CreatedAt)
	w.mu.Lock()
	w.session = session
	w.requestedName = requestedName
	w.duration = duration
	w.mu.Unlock()

	w.sessionName.SetText(info.Name)
	w.sessionName.Disable()
	w.setError("")
	w.startButton.Disable()
	w.stopButton.Enable()
	for _, source := range w.selectedSources() {
		if err := source.start(paths[source]); err != nil {
			sourceName := "speaker"
			if source.isMicrophone {
				sourceName = "microphone"
			}
			log.Printf("start %s recording: %v", sourceName, err)
		}
	}
	w.finishIfIdle()
}

func (w *RecorderWin) stop() {
	w.mu.Lock()
	active := w.session != nil
	w.mu.Unlock()
	if !active {
		return
	}
	w.stopButton.Disable()
	for _, source := range w.sources {
		source.stop()
	}
	w.finishIfIdle()
}

func (w *RecorderWin) finishIfIdle() {
	w.mu.Lock()
	active := w.session != nil
	w.mu.Unlock()
	if !active {
		return
	}
	for _, source := range w.sources {
		if source.currentCapture() != nil {
			return
		}
	}
	w.finishSession()
}

func (w *RecorderWin) finishSession() {
	w.mu.Lock()
	session := w.session
	if session == nil {
		w.mu.Unlock()
		return
	}
	requestedName := w.requestedName
	duration := w.duration
	w.session = nil
	w.requestedName = ""
	w.duration = nil
	w.mu.Unlock()
	duration.Stop()

	status := SessionStatusStopped
	var taskErr error
	for _, source := range w.sources {
		if err := source.lastError(); err != nil {
			status = SessionStatusFailed
			if taskErr == nil {
				taskErr = fmt.Errorf("%s: %w", source.title, err)
			}
		}
	}
	finishErr := session.Finish(status, nil, taskErr)
	refreshActiveSessionHistory()
	if w.closed.Load() {
		return
	}

	w.sessionName.SetText(requestedName)
	w.sessionName.Enable()
	if len(w.selectedSources()) > 0 {
		w.startButton.Enable()
	}
	w.stopButton.Disable()
	if finishErr != nil {
		w.setError("错误：保存会话失败：" + finishErr.Error())
	} else if taskErr != nil {
		w.setError("错误：" + taskErr.Error())
	}
}

func (w *RecorderWin) setError(text string) {
	w.errorLabel.SetText(text)
	if text == "" {
		w.errorLabel.Hide()
		return
	}
	w.errorLabel.Show()
}

func (w *RecorderWin) stopAllAndWait() {
	var wg sync.WaitGroup
	for _, source := range w.sources {
		capture := source.currentCapture()
		if capture == nil {
			continue
		}
		wg.Add(1)
		go func(capture *sound_cap.SoundCap) {
			defer wg.Done()
			capture.Close()
		}(capture)
	}
	wg.Wait()
	w.finishSession()
}

func newRecordSource(parent *RecorderWin, title, deviceName string,
	mode sound_cap.CaptureMode, isMicrophone, selected bool) *recordSource {
	s := &recordSource{
		parent:       parent,
		title:        title,
		deviceName:   deviceName,
		mode:         mode,
		isMicrophone: isMicrophone,
		selected:     selected,
		status:       widget.NewLabel("未录音"),
		errorLabel:   widget.NewLabel(""),
		filePath:     widget.NewMultiLineEntry(),
	}
	s.errorLabel.Wrapping = fyne.TextWrapBreak
	s.setError("")
	s.filePath.Wrapping = fyne.TextWrapBreak
	s.filePath.SetMinRowsVisible(2)
	s.filePath.SetPlaceHolder("开始录音后显示完整文件路径")
	s.filePath.Disable()
	if !selected {
		s.status.SetText("未选中")
		s.filePath.SetPlaceHolder("未选中，不生成录音文件")
	}
	return s
}

func (s *recordSource) content() fyne.CanvasObject {
	deviceName := s.deviceName
	if deviceName == "" {
		deviceName = "系统默认（开始时检测）"
	}
	saveFileLabel := widget.NewLabel("保存文件：")
	if !s.selected {
		for _, label := range []*widget.Label{s.status, saveFileLabel} {
			label.Importance = widget.LowImportance
		}
	}
	return widget.NewCard(
		s.title,
		"设备："+deviceName,
		container.NewVBox(
			container.NewBorder(nil, nil, nil, s.status),
			saveFileLabel,
			s.filePath,
			s.errorLabel,
		),
	)
}

func (s *recordSource) setError(text string) {
	s.errorLabel.SetText(text)
	if text == "" {
		s.errorLabel.Hide()
		return
	}
	s.errorLabel.Show()
}

func (s *recordSource) start(path string) error {
	if !s.selected || s.parent.closed.Load() {
		return nil
	}
	s.mu.Lock()
	if s.capture != nil {
		s.mu.Unlock()
		return nil
	}
	s.lastErr = nil
	s.mu.Unlock()

	s.filePath.SetText(path)
	s.setError("")
	s.status.SetText("正在启动...")
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
		s.mu.Lock()
		s.lastErr = err
		s.mu.Unlock()
		s.status.SetText("录音失败")
		s.setError("错误：" + err.Error())
		return err
	}

	s.mu.Lock()
	s.capture = capture
	s.mu.Unlock()
	if s.parent.closed.Load() {
		go func() {
			capture.Close()
			s.onRunFinished(capture, nil)
		}()
		return nil
	}
	go func() {
		err := capture.Run()
		s.onRunFinished(capture, err)
	}()
	return nil
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

func (s *recordSource) lastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
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
			s.setError("错误：" + err.Error())
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
		case sound_cap.StatePaused:
			s.status.SetText("已暂停")
		case sound_cap.StateResuming:
			s.status.SetText("正在恢复...")
		case sound_cap.StateStopped:
			if s.lastError() != nil {
				s.status.SetText("已停止（有错误）")
			} else {
				s.status.SetText("已停止")
			}
		case sound_cap.StateError:
			s.status.SetText("录音失败")
		}
	})
}

func (s *recordSource) onRunFinished(capture *sound_cap.SoundCap, runErr error) {
	fyne.Do(func() {
		s.mu.Lock()
		if s.capture != capture {
			s.mu.Unlock()
			return
		}
		s.capture = nil
		if runErr != nil && s.lastErr == nil {
			s.lastErr = runErr
		}
		lastErr := s.lastErr
		s.mu.Unlock()

		if !s.parent.closed.Load() {
			switch {
			case capture.State() == sound_cap.StateError:
				s.status.SetText("录音失败")
			case lastErr != nil:
				s.status.SetText("已停止（有错误）")
			default:
				s.status.SetText("已停止")
			}
			if capture.State() == sound_cap.StateError && s.errorLabel.Text == "" {
				if runErr != nil {
					s.setError("错误：" + runErr.Error())
				} else {
					s.setError("错误：录音失败")
				}
			}
		}
		s.parent.finishIfIdle()
	})
}
