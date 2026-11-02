package gui

import (
	"testing"
	"time"

	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestFormatSessionDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{name: "negative", duration: -time.Second, want: "00:00:00"},
		{name: "seconds", duration: 61 * time.Second, want: "00:01:01"},
		{name: "hours", duration: 3661 * time.Second, want: "01:01:01"},
		{name: "multiple days", duration: 50*time.Hour + 2*time.Minute, want: "50:02:00"},
		{name: "truncate fraction", duration: 1500 * time.Millisecond, want: "00:00:01"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := formatSessionDuration(test.duration); got != test.want {
				t.Fatalf("formatSessionDuration(%v) = %q, want %q", test.duration, got, test.want)
			}
		})
	}
}

func TestSessionDurationViewPauseAndResume(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	view := newSessionDurationView(widget.NewLabel(""), time.Now())
	defer view.Stop()

	time.Sleep(20 * time.Millisecond)
	view.Pause()
	pausedAt := view.elapsedAt(time.Now())
	time.Sleep(20 * time.Millisecond)
	if elapsed := view.elapsedAt(time.Now()); elapsed-pausedAt > 5*time.Millisecond {
		t.Fatalf("duration kept increasing while paused: before=%v after=%v", pausedAt, elapsed)
	}

	view.Resume()
	time.Sleep(20 * time.Millisecond)
	if elapsed := view.elapsedAt(time.Now()); elapsed-pausedAt < 10*time.Millisecond {
		t.Fatalf("duration did not resume: before=%v after=%v", pausedAt, elapsed)
	}
}

func TestSessionDurationViewStop(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	label := widget.NewLabel("")
	view := newSessionDurationView(label, time.Now())
	if label.Text != "时长：00:00:00" {
		t.Fatalf("initial duration = %q", label.Text)
	}
	view.Stop()
	view.Stop()
	if view.active.Load() {
		t.Fatal("duration view remained active after Stop")
	}
}
