package gui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"xaux_gui/pkg/sound_cap"
	"xaux_gui/pkg/vad"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var activeVADTest struct {
	sync.Mutex
	window *vadTestWindow
}

type vadTestRun struct {
	scope  *vadScope
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

func (r *vadTestRun) report(err error) {
	if err != nil {
		log.Printf("VAD microphone test: %v", err)
		r.mu.Lock()
		if r.err == nil {
			r.err = err
		}
		r.mu.Unlock()
	}
}

func (r *vadTestRun) failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

type vadTestWindow struct {
	window       fyne.Window
	locale       localeBindings
	status       *widget.Label
	stats        *widget.Label
	device       *widget.Label
	startButton  *widget.Button
	stopButton   *widget.Button
	plot         vadPlot
	raster       *canvas.Raster
	run          *vadTestRun
	stopping     bool
	closed       atomic.Bool
	shutdownOnce sync.Once
	shutdownDone chan struct{}
}

func runVADTest() {
	activeVADTest.Lock()
	window := activeVADTest.window
	activeVADTest.Unlock()
	if window != nil {
		window.window.Show()
		window.window.RequestFocus()
		return
	}
	window = newVADTestWindow()
	activeVADTest.Lock()
	activeVADTest.window = window
	activeVADTest.Unlock()
	window.window.Show()
}

func newVADTestWindow() *vadTestWindow {
	s := &vadTestWindow{shutdownDone: make(chan struct{})}
	s.window = fyne.CurrentApp().NewWindow(tr("vad.title"))
	s.status, s.stats, s.device = widget.NewLabel(""), widget.NewLabel(""), widget.NewLabel("")
	s.status.Wrapping = fyne.TextWrapWord
	s.device.Wrapping = fyne.TextWrapWord
	s.stats.Wrapping = fyne.TextWrapWord
	s.startButton = widget.NewButtonWithIcon("", theme.MediaPlayIcon(), s.start)
	s.startButton.Importance = widget.HighImportance
	s.stopButton = widget.NewButtonWithIcon("", theme.MediaStopIcon(), s.stop)
	s.stopButton.Disable()
	s.locale.setButton(s.startButton, "vad.start", nil)
	s.locale.setButton(s.stopButton, "vad.stop", nil)
	s.locale.setLabel(s.status, "vad.idle", nil)
	s.locale.setLabel(s.device, "vad.device_hint", nil)
	if !vad.Enabled {
		s.startButton.Disable()
		s.locale.setLabel(s.status, "vad.unavailable", nil)
	}
	intro, legend, axis := widget.NewLabel(""), widget.NewLabel(""), widget.NewLabel("")
	intro.Wrapping, legend.Wrapping = fyne.TextWrapWord, fyne.TextWrapWord
	s.locale.setLabel(intro, "vad.intro", nil)
	s.locale.setLabel(legend, "vad.legend", nil)
	s.locale.setLabel(axis, "vad.axis", nil)
	s.raster = canvas.NewRaster(s.plot.render)
	s.raster.SetMinSize(fyne.NewSize(640, 280))
	s.window.SetContent(container.NewPadded(container.NewBorder(
		container.NewVBox(intro, s.device, container.NewHBox(s.startButton, s.stopButton), s.status, s.stats),
		container.NewVBox(axis, legend), nil, nil, s.raster,
	)))
	s.locale.add(func() {
		s.window.SetTitle(tr("vad.title"))
		s.refresh()
	})
	s.locale.start()
	s.window.Resize(fyne.NewSize(900, 600))
	s.window.SetCloseIntercept(s.shutdown)
	return s
}

func (s *vadTestWindow) start() {
	if s.run != nil || s.closed.Load() {
		return
	}
	if !acquireVoiceTask(vadMenuName) {
		showLocalizedInformation(tr("menu.vad"),
			trData("vad.active_task", map[string]any{"Task": voiceTaskDisplayName(activeVoiceTask())}), s.window)
		return
	}
	setting, err := currentAppSetting()
	if err != nil {
		releaseVoiceTask(vadMenuName)
		showLocalizedError(err, s.window)
		return
	}
	detector, err := vad.New()
	if err != nil {
		releaseVoiceTask(vadMenuName)
		showLocalizedError(err, s.window)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &vadTestRun{scope: &vadScope{detector: detector}, cancel: cancel, done: make(chan struct{})}
	// No proxy address and no recording path: this path cannot start cloud ASR or save audio.
	capture, err := sound_cap.NewSoundCap(ctx, &sound_cap.Config{
		Mode: sound_cap.CaptureMicrophone, DeviceName: setting.microphoneName,
		OnPCM: run.scope.process, OnError: run.report,
	}, nil)
	if err != nil {
		cancel()
		err = errors.Join(err, detector.Close())
		releaseVoiceTask(vadMenuName)
		showLocalizedError(err, s.window)
		return
	}
	s.run, s.stopping = run, false
	s.startButton.Disable()
	s.stopButton.Enable()
	s.locale.setLabel(s.status, "common.starting", nil)
	name := setting.microphoneName
	s.locale.setLabel(s.device, "vad.device", func() any {
		displayName := name
		if displayName == "" {
			displayName = tr("record.system_default")
		}
		return map[string]any{"Name": displayName}
	})
	s.refresh()
	go func() {
		run.report(capture.Run())
		capture.Close()
		run.report(detector.Close())
		cancel()
		releaseVoiceTask(vadMenuName)
		close(run.done)
	}()
	go s.watch(run)
}

func (s *vadTestWindow) watch(run *vadTestRun) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var queued atomic.Bool
	for {
		select {
		case <-run.done:
			fyne.Do(func() {
				if s.closed.Load() || s.run != run {
					return
				}
				s.refresh()
				s.run = nil
				s.stopping = false
				s.startButton.Enable()
				s.stopButton.Disable()
				if err := run.failure(); err != nil {
					s.locale.setLabel(s.status, "common.error", map[string]any{"Error": err})
				} else {
					s.locale.setLabel(s.status, "common.stopped", nil)
				}
			})
			return
		case <-ticker.C:
			if queued.CompareAndSwap(false, true) {
				fyne.Do(func() {
					defer queued.Store(false)
					if !s.closed.Load() && s.run == run {
						s.refresh()
					}
				})
			}
		}
	}
}

func (s *vadTestWindow) refresh() {
	if s.run == nil {
		return
	}
	snapshot := s.run.scope.snapshot()
	s.plot.setFrames(snapshot.frames)
	s.raster.Refresh()
	data := map[string]any{
		"Total": fmt.Sprintf("%.1f", float64(snapshot.total)*0.016),
		"Voice": fmt.Sprintf("%.1f", float64(snapshot.voiced)*0.016),
		"Ratio": fmt.Sprintf("%.0f", 100*float64(snapshot.voiced)/float64(max(1, snapshot.total))),
		"Level": "-96",
	}
	if len(snapshot.frames) > 0 {
		last := snapshot.frames[len(snapshot.frames)-1]
		data["Level"] = fmt.Sprintf("%.0f", last.level)
		if !s.stopping {
			key := "vad.quiet"
			if last.speech {
				key = "vad.voice"
			}
			s.locale.setLabel(s.status, key, nil)
		}
	}
	s.locale.setLabel(s.stats, "vad.stats", data)
	if err := s.run.failure(); err != nil {
		s.locale.setLabel(s.status, "common.error", map[string]any{"Error": err})
	}
}

func (s *vadTestWindow) stop() {
	if s.run == nil || s.stopping {
		return
	}
	s.stopping = true
	s.stopButton.Disable()
	s.locale.setLabel(s.status, "common.stopping", nil)
	s.run.cancel()
}

func (s *vadTestWindow) shutdown() {
	s.shutdownOnce.Do(func() {
		s.closed.Store(true)
		s.locale.stop()
		var done <-chan struct{}
		if s.run != nil {
			s.run.cancel()
			done = s.run.done
		}
		go func() {
			if done != nil {
				<-done
			}
			fyne.DoAndWait(func() {
				s.window.SetCloseIntercept(nil)
				s.window.Close()
				activeVADTest.Lock()
				if activeVADTest.window == s {
					activeVADTest.window = nil
				}
				activeVADTest.Unlock()
			})
			close(s.shutdownDone)
		}()
	})
}

func stopActiveVADTest() <-chan struct{} {
	activeVADTest.Lock()
	window := activeVADTest.window
	activeVADTest.Unlock()
	if window != nil {
		window.shutdown()
		return window.shutdownDone
	}
	return nil
}
