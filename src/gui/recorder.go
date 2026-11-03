package gui

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"xaux_gui/pkg/sound_cap"
	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
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
	locale        *localeBindings

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
			dialog.Message("%v", info).Title(tr("common.open_error")).Error()
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
		showLocalizedInformation(
			tr("menu.record"),
			trData("record.active_task", map[string]any{"Task": voiceTaskDisplayName(activeTask)}),
			currentMainWin.win,
		)
		return
	}
	if !acquireVoiceTask(recordMenuName) {
		showLocalizedInformation(
			tr("menu.record"),
			tr("record.active_generic"),
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
		durationLabel: widget.NewLabel(trData("common.duration", map[string]any{"Duration": "--:--:--"})),
		errorLabel:    widget.NewLabel(""),
		locale:        &localeBindings{},
	}
	win.locale.add(func() { win.sessionName.SetPlaceHolder(tr("record.session_placeholder")) })
	win.locale.setLabel(win.durationLabel, "common.duration", map[string]any{"Duration": "--:--:--"})
	win.errorLabel.Wrapping = fyne.TextWrapBreak
	win.setError("")
	win.window = fyne.CurrentApp().NewWindow(tr("menu.record"))
	win.locale.add(func() { win.window.SetTitle(tr("menu.record")) })
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
		win.locale.stop()
		win.closed.Store(true)
		win.stopAllAndWait()
		win.release()
	})
	recorderWindows.Lock()
	recorderWindows.active = win
	recorderWindows.Unlock()
	win.window.Show()
	win.locale.start()
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
	intro := widget.NewLabel("")
	w.locale.setLabel(intro, "record.intro", nil)
	intro.Wrapping = fyne.TextWrapBreak
	saveDir := widget.NewLabel("")
	w.locale.setLabel(saveDir, "record.session_root", map[string]any{"Path": w.saveDir})
	saveDir.Wrapping = fyne.TextWrapBreak

	w.startButton = widget.NewButtonWithIcon("", mytheme.ResBegin, w.start)
	w.locale.setButton(w.startButton, "record.start", nil)
	w.startButton.Importance = widget.HighImportance
	w.stopButton = widget.NewButtonWithIcon("", mytheme.ResEnd, w.stop)
	w.locale.setButton(w.stopButton, "record.stop", nil)
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

	sessionLabel := widget.NewLabel("")
	w.locale.setLabel(sessionLabel, "record.session_name", nil)
	body := container.NewVBox(
		intro,
		saveDir,
		widget.NewSeparator(),
		container.NewBorder(nil, nil, sessionLabel, w.durationLabel, w.sessionName),
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
		w.setLocalizedError("common.error", map[string]any{"Error": err})
		return
	}
	info := session.Snapshot()
	w.locale.forgetLabel(w.durationLabel)
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
	var failedSource *recordSource
	for _, source := range w.sources {
		if err := source.lastError(); err != nil {
			status = SessionStatusFailed
			if taskErr == nil {
				taskErr = fmt.Errorf("%s: %w", source.title, err)
				failedSource = source
			}
		}
	}
	finishErr := session.Finish(status, nil, taskErr)
	refreshActiveSessionHistory()
	if w.closed.Load() {
		return
	}
	if duration != nil && w.durationLabel != nil {
		w.locale.setLabel(w.durationLabel, "common.duration", map[string]any{
			"Duration": formatSessionDuration(duration.elapsedAt(time.Now())),
		})
	}

	w.sessionName.SetText(requestedName)
	w.sessionName.Enable()
	if len(w.selectedSources()) > 0 {
		w.startButton.Enable()
	}
	w.stopButton.Disable()
	if finishErr != nil {
		w.setLocalizedError("record.save_failed", map[string]any{"Error": finishErr})
	} else if taskErr != nil {
		w.setLocalizedError("record.source_error", func() any {
			key := "record.speaker"
			if failedSource.isMicrophone {
				key = "record.microphone"
			}
			return map[string]any{"Source": tr(key), "Error": failedSource.lastError()}
		})
	}
}

func (w *RecorderWin) setError(text string) {
	if text == "" {
		w.locale.clearLabel(w.errorLabel)
		w.errorLabel.Hide()
		return
	}
	w.errorLabel.SetText(text)
	w.errorLabel.Show()
}

func (w *RecorderWin) setLocalizedError(key string, data any) {
	w.locale.setLabel(w.errorLabel, key, data)
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
		status:       widget.NewLabel(""),
		errorLabel:   widget.NewLabel(""),
		filePath:     widget.NewMultiLineEntry(),
	}
	s.errorLabel.Wrapping = fyne.TextWrapBreak
	s.setError("")
	parent.locale.setLabel(s.status, "record.idle", nil)
	s.filePath.Wrapping = fyne.TextWrapBreak
	s.filePath.SetMinRowsVisible(2)
	placeholder := "record.file_placeholder"
	s.filePath.Disable()
	if !selected {
		parent.locale.setLabel(s.status, "record.not_selected", nil)
		placeholder = "record.not_selected_placeholder"
	}
	parent.locale.add(func() { s.filePath.SetPlaceHolder(tr(placeholder)) })
	return s
}

func (s *recordSource) content() fyne.CanvasObject {
	saveFileLabel := widget.NewLabel("")
	s.parent.locale.setLabel(saveFileLabel, "record.save_file", nil)
	if !s.selected {
		for _, label := range []*widget.Label{s.status, saveFileLabel} {
			label.Importance = widget.LowImportance
		}
	}
	card := widget.NewCard(
		"", "",
		container.NewVBox(
			container.NewBorder(nil, nil, nil, s.status),
			saveFileLabel,
			s.filePath,
			s.errorLabel,
		),
	)
	s.parent.locale.add(func() {
		title := "record.speaker"
		if s.isMicrophone {
			title = "record.microphone"
		}
		deviceName := s.deviceName
		if deviceName == "" {
			deviceName = tr("record.system_default")
		}
		card.SetTitle(tr(title))
		card.SetSubTitle(trData("record.device", map[string]any{"Name": deviceName}))
	})
	return card
}

func (s *recordSource) setError(text string) {
	if text == "" {
		s.parent.locale.clearLabel(s.errorLabel)
		s.errorLabel.Hide()
		return
	}
	s.errorLabel.SetText(text)
	s.errorLabel.Show()
}

func (s *recordSource) setLocalizedError(key string, data any) {
	s.parent.locale.setLabel(s.errorLabel, key, data)
	s.errorLabel.Show()
}

func (s *recordSource) setStatus(key string) {
	s.parent.locale.setLabel(s.status, key, nil)
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
	s.setStatus("common.starting")
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
		s.setStatus("record.failed")
		s.setLocalizedError("common.error", map[string]any{"Error": err})
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
	s.setStatus("common.stopping")
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
			s.setLocalizedError("common.error", map[string]any{"Error": err})
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
			s.setStatus("common.starting")
		case sound_cap.StateRunning:
			s.setStatus("record.active")
		case sound_cap.StatePaused:
			s.setStatus("common.paused")
		case sound_cap.StateResuming:
			s.setStatus("common.resuming")
		case sound_cap.StateStopped:
			if s.lastError() != nil {
				s.setStatus("common.stopped_error")
			} else {
				s.setStatus("common.stopped")
			}
		case sound_cap.StateError:
			s.setStatus("record.failed")
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
				s.setStatus("record.failed")
			case lastErr != nil:
				s.setStatus("common.stopped_error")
			default:
				s.setStatus("common.stopped")
			}
			if capture.State() == sound_cap.StateError && s.errorLabel.Text == "" {
				if runErr != nil {
					s.setLocalizedError("common.error", map[string]any{"Error": runErr})
				} else {
					s.setLocalizedError("record.error", nil)
				}
			}
		}
		s.parent.finishIfIdle()
	})
}
