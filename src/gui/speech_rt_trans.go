package gui

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
)

var activeSpeech struct {
	sync.Mutex
	window *speechRTTrans
}

type transcriptItem struct {
	deviceName   string
	isMicrophone bool
	timeString   string
	content      string
	final        bool
}

type transcriptRow struct {
	content   *widget.Label
	status    *widget.Label
	container *fyne.Container
}

type speechRTTrans struct {
	window           fyne.Window
	listItems        []*transcriptItem
	rows             []*transcriptRow
	transcript       *fyne.Container
	emptyState       *widget.Label
	scroll           *container.Scroll
	status           *widget.Label
	duration         *sessionDurationView
	pauseButton      *widget.Button
	stopButton       *widget.Button
	latestButton     *widget.Button
	running          atomic.Bool
	stopping         atomic.Bool
	releaseOnce      sync.Once
	shutdownOnce     sync.Once
	onClosed         func()
	onPause          func() error
	onResume         func() error
	shutdownDone     chan struct{}
	sessionStartedAt time.Time
	runMu            sync.Mutex
	runErr           error
	paused           bool
	transitioning    bool
	stopRequested    bool
	followLatest     bool
	locale           *localeBindings
}

func newSpeechRTTrans() *speechRTTrans {
	if !acquireVoiceTask(realTimeTransMenuName) {
		return nil
	}
	return &speechRTTrans{followLatest: true, shutdownDone: make(chan struct{}), locale: &localeBindings{}}
}

func (s *speechRTTrans) updateASRResult(capture *asrSoundCap, response *x.AllResponse) {
	if capture == nil || response == nil {
		return
	}
	if response.Type == x.TypeSentenceStart {
		capture.acceptASRResult(response)
		fyne.Do(func() {
			capture.resultIndex = -1
			capture.writeListIndex = -1
		})
		return
	}
	if response.Type != x.TypeRecognizing && response.Type != x.TypeSentenceEnd {
		return
	}
	if accepted, _ := capture.acceptASRResult(response); !accepted {
		return
	}
	fyne.Do(func() {
		s.applyASRResult(capture, response)
	})
}

func (s *speechRTTrans) applyASRResult(capture *asrSoundCap, response *x.AllResponse) {
	if !s.running.Load() {
		return
	}
	if s.transcript == nil || s.scroll == nil {
		return
	}

	resultIndex := response.Result.Index
	content := response.Result.Result
	sameResult := capture.resultIndex == resultIndex &&
		capture.writeListIndex >= 0 && capture.writeListIndex < len(s.listItems)
	if content == "" && !sameResult {
		return
	}
	isFinal := asrResponseIsFinal(response)
	if sameResult {
		index := capture.writeListIndex
		item := s.listItems[index]
		if content != "" {
			item.content = content
		}
		item.final = isFinal
		s.updateTranscriptRow(index, item)
	} else {
		item := &transcriptItem{
			deviceName:   capture.deviceName,
			isMicrophone: capture.isMicrophone,
			timeString:   time.Now().Format("2006-01-02 15:04:05"),
			content:      content,
			final:        isFinal,
		}
		capture.writeListIndex = len(s.listItems)
		if len(s.listItems) == 0 {
			s.transcript.Objects = nil
		}
		s.listItems = append(s.listItems, item)
		s.rows = append(s.rows, s.newTranscriptRow(item))
		if len(s.rows) > 1 {
			s.transcript.Objects = append(s.transcript.Objects, widget.NewSeparator())
		}
		s.transcript.Objects = append(s.transcript.Objects, s.rows[len(s.rows)-1].container)
	}
	capture.resultIndex = resultIndex
	s.transcript.Refresh()
	s.scroll.Refresh()
	s.updateTranscriptFollow()
}

func (s *speechRTTrans) newTranscriptRow(item *transcriptItem) *transcriptRow {
	icon := widget.NewIcon(mytheme.ResSpeakerIcon)
	if item.isMicrophone {
		icon.SetResource(mytheme.ResMicrophoneIcon)
	}
	timeLabel := widget.NewLabel(fmt.Sprintf("%s  %s", item.deviceName, item.timeString))
	timeLabel.TextStyle.Bold = true
	status := widget.NewLabel("")
	status.Importance = widget.LowImportance
	content := widget.NewLabel(item.content)
	content.Wrapping = fyne.TextWrapBreak
	content.Selectable = true
	row := container.NewVBox(
		container.NewHBox(icon, timeLabel, layout.NewSpacer(), status),
		container.NewPadded(content),
	)
	result := &transcriptRow{content: content, status: status}
	result.container = row
	s.setTranscriptRowStatus(result, item)
	return result
}

func (s *speechRTTrans) updateTranscriptRow(index int, item *transcriptItem) {
	row := s.rows[index]
	row.content.SetText(item.content)
	s.setTranscriptRowStatus(row, item)
}

func (s *speechRTTrans) setTranscriptRowStatus(row *transcriptRow, item *transcriptItem) {
	if item.final {
		s.locale.clearLabel(row.status)
		return
	}
	s.locale.setLabel(row.status, "speech.recognizing", nil)
}

func transcriptNearBottom(contentHeight, viewportHeight, offsetY float32) bool {
	return contentHeight-offsetY <= viewportHeight+32
}

func (s *speechRTTrans) updateTranscriptFollow() {
	if s.followLatest {
		s.scroll.ScrollToBottom()
		s.latestButton.Hide()
		return
	}
	s.latestButton.Show()
}

func (s *speechRTTrans) setRunError(err error) {
	if err == nil {
		return
	}
	s.runMu.Lock()
	if s.runErr == nil {
		s.runErr = err
	}
	s.runMu.Unlock()
}

func (s *speechRTTrans) setCaptureError(err error) {
	if err == nil {
		return
	}
	s.setRunError(err)
	log.Printf("real-time transcription: %v", err)
	fyne.Do(func() {
		if s.status != nil && !s.stopping.Load() {
			s.locale.setLabel(s.status, "speech.capture_error", map[string]any{"Error": err})
			s.status.Importance = widget.DangerImportance
			s.status.Refresh()
		}
		if s.pauseButton != nil {
			s.pauseButton.Disable()
		}
	})
}

func (s *speechRTTrans) runError() error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return s.runErr
}

func (s *speechRTTrans) transcriptEntries() []SessionTranscriptEntry {
	entries := make([]SessionTranscriptEntry, 0, len(s.listItems))
	for _, item := range s.listItems {
		entries = append(entries, SessionTranscriptEntry{
			Timestamp: item.timeString,
			Device:    item.deviceName,
			Content:   item.content,
		})
	}
	return entries
}

func (s *speechRTTrans) start(onClosed func(), onPause func() error, onResume func() error) {
	s.onClosed = onClosed
	s.onPause = onPause
	s.onResume = onResume

	s.status = widget.NewLabel("")
	s.locale.setLabel(s.status, "speech.listening", nil)
	s.status.Importance = widget.SuccessImportance
	startedAt := s.sessionStartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	s.duration = newSessionDurationView(widget.NewLabel(""), startedAt)
	s.pauseButton = widget.NewButton("", s.togglePause)
	s.locale.setButton(s.pauseButton, "speech.pause", nil)
	s.stopButton = widget.NewButtonWithIcon("", mytheme.ResEnd, s.shutdown)
	s.locale.setButton(s.stopButton, "speech.stop_save", nil)
	s.latestButton = widget.NewButton("", func() {
		s.followLatest = true
		s.scroll.ScrollToBottom()
		s.latestButton.Hide()
	})
	s.locale.setButton(s.latestButton, "speech.latest", nil)
	s.latestButton.Hide()

	toolbar := container.NewHBox(
		s.status,
		layout.NewSpacer(),
		s.duration.label,
		s.pauseButton,
		s.stopButton,
	)
	s.transcript = container.NewVBox()
	s.emptyState = widget.NewLabel("")
	s.locale.setLabel(s.emptyState, "speech.waiting", nil)
	s.emptyState.Importance = widget.LowImportance
	s.transcript.Objects = []fyne.CanvasObject{container.NewCenter(s.emptyState)}
	s.scroll = container.NewVScroll(s.transcript)
	s.scroll.OnScrolled = func(position fyne.Position) {
		if s.scroll == nil || s.transcript == nil {
			return
		}
		s.followLatest = transcriptNearBottom(
			s.transcript.MinSize().Height,
			s.scroll.Size().Height,
			position.Y,
		)
		if s.followLatest {
			s.latestButton.Hide()
		} else {
			s.latestButton.Show()
		}
	}
	latestBar := container.NewHBox(layout.NewSpacer(), s.latestButton)
	content := container.NewBorder(
		container.NewPadded(toolbar),
		container.NewPadded(latestBar),
		nil, nil,
		s.scroll,
	)
	s.window = fyne.CurrentApp().NewWindow(tr("speech.title"))
	s.locale.add(func() { s.window.SetTitle(tr("speech.title")) })
	s.window.SetContent(content)
	s.window.Resize(fyne.NewSize(960, 640))
	s.window.CenterOnScreen()
	s.window.SetCloseIntercept(s.shutdown)
	s.window.SetOnClosed(s.release)
	s.running.Store(true)
	activeSpeech.Lock()
	activeSpeech.window = s
	activeSpeech.Unlock()
	s.window.Show()
	s.locale.start()
}

func (s *speechRTTrans) togglePause() {
	if !s.running.Load() || s.stopping.Load() || s.transitioning {
		return
	}
	s.transitioning = true
	s.pauseButton.Disable()
	pause := !s.paused
	if pause {
		s.locale.setLabel(s.status, "speech.pausing", nil)
	} else {
		s.locale.setLabel(s.status, "speech.resuming", nil)
	}
	action := s.onPause
	if !pause {
		action = s.onResume
	}
	go func() {
		var err error
		if action != nil {
			err = action()
		}
		fyne.Do(func() {
			s.transitioning = false
			if s.stopping.Load() {
				return
			}
			if err != nil {
				s.setRunError(err)
				log.Printf("pause/resume real-time transcription: %v", err)
				s.locale.setLabel(s.status, "speech.action_failed", map[string]any{"Error": err})
				s.status.Importance = widget.DangerImportance
				s.status.Refresh()
				if s.paused {
					s.locale.setButton(s.pauseButton, "speech.resume", nil)
				} else {
					s.locale.setButton(s.pauseButton, "speech.pause", nil)
				}
				s.pauseButton.Disable()
				if s.stopRequested {
					s.stopRequested = false
					s.shutdown()
				}
				return
			}
			s.paused = pause
			if s.paused {
				s.locale.setLabel(s.status, "common.paused", nil)
				s.locale.setButton(s.pauseButton, "speech.resume", nil)
				s.duration.Pause()
			} else {
				s.locale.setLabel(s.status, "speech.listening", nil)
				s.locale.setButton(s.pauseButton, "speech.pause", nil)
				s.duration.Resume()
			}
			s.status.Importance = widget.SuccessImportance
			if s.paused {
				s.status.Importance = widget.WarningImportance
			}
			s.status.Refresh()
			s.pauseButton.Enable()
			if s.stopRequested {
				s.stopRequested = false
				s.shutdown()
			}
		})
	}()
}

func (s *speechRTTrans) shutdown() {
	if s.transitioning {
		s.stopRequested = true
		s.stopButton.Disable()
		return
	}
	s.shutdownOnce.Do(func() {
		s.stopping.Store(true)
		if s.status != nil {
			s.locale.setLabel(s.status, "speech.saving", nil)
			s.status.Importance = widget.MediumImportance
			s.status.Refresh()
		}
		if len(s.listItems) == 0 && s.emptyState != nil {
			s.locale.setLabel(s.emptyState, "speech.no_content", nil)
		}
		if s.pauseButton != nil {
			s.pauseButton.Disable()
		}
		if s.stopButton != nil {
			s.stopButton.Disable()
		}
		if s.duration != nil {
			s.duration.Stop()
			s.duration = nil
		}
		go func() {
			if s.onClosed != nil {
				s.onClosed()
			}
			fyne.DoAndWait(func() {
				s.running.Store(false)
				if s.window != nil {
					s.window.SetCloseIntercept(nil)
					s.window.Close()
				}
				s.release()
				refreshActiveSessionHistory()
			})
			close(s.shutdownDone)
		}()
	})
}

func (s *speechRTTrans) release() {
	s.releaseOnce.Do(func() {
		s.locale.stop()
		activeSpeech.Lock()
		if activeSpeech.window == s {
			activeSpeech.window = nil
		}
		activeSpeech.Unlock()
		releaseVoiceTask(realTimeTransMenuName)
	})
}

func stopActiveTranscription() <-chan struct{} {
	activeSpeech.Lock()
	window := activeSpeech.window
	activeSpeech.Unlock()
	if window != nil {
		window.shutdown()
		return window.shutdownDone
	}
	return nil
}
