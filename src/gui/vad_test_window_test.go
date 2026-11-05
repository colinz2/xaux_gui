package gui

import (
	"context"
	"strings"
	"testing"
	"time"

	"xaux_gui/pkg/vad"

	fynetest "fyne.io/fyne/v2/test"
)

func TestVADWindowLanguageAndLocalPlot(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := uiLocale.setLanguage(localeChinese); err != nil {
			t.Error(err)
		}
	}()
	window := newVADTestWindow()
	defer func() {
		window.shutdown()
		select {
		case <-window.shutdownDone:
		case <-time.After(3 * time.Second):
			t.Error("VAD window did not close")
		}
	}()
	window.window.Show()
	if window.window.Title() != "VAD · 麦克风实验室" || window.startButton.Disabled() == vad.Enabled ||
		!window.stopButton.Disabled() {
		t.Fatal("wrong initial VAD window state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &vadTestRun{scope: &vadScope{detector: plotTestDetector{}}, cancel: cancel, done: make(chan struct{})}
	defer cancel()
	close(run.done)
	window.run = run
	if err := run.scope.process(plotTestPCM(16384, 10)); err != nil {
		t.Fatal(err)
	}
	window.refresh()
	if !strings.Contains(window.status.Text, "有人声") || !strings.Contains(window.stats.Text, "100%") {
		t.Fatal("voice state / ratio did not reach the UI")
	}
	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if window.window.Title() != "VAD · Microphone lab" || window.startButton.Text != "Start test" ||
		!strings.Contains(window.status.Text, "VOICE") || run.scope.snapshot().total != 10 {
		t.Fatal("language switch lost the live plot or did not translate controls")
	}
	window.stop()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("stop did not cancel local capture")
	}
	if !window.stopButton.Disabled() || window.status.Text != "Stopping..." {
		t.Fatal("stop controls not updated")
	}
}

func TestVADWindowSingletonAndShutdown(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	runVADTest()
	activeVADTest.Lock()
	first := activeVADTest.window
	activeVADTest.Unlock()
	runVADTest()
	activeVADTest.Lock()
	second := activeVADTest.window
	activeVADTest.Unlock()
	if first == nil || first != second {
		t.Fatal("VAD menu opened duplicate windows")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := &vadTestRun{cancel: cancel, done: make(chan struct{})}
	first.run = run
	done := stopActiveVADTest()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("application shutdown did not cancel microphone capture")
	}
	select {
	case <-done:
		t.Fatal("window closed before capture was released")
	default:
	}
	close(run.done)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("application shutdown did not close VAD window")
	}
	activeVADTest.Lock()
	defer activeVADTest.Unlock()
	if activeVADTest.window != nil {
		t.Fatal("closed VAD window remained registered")
	}
}
