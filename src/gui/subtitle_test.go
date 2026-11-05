package gui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"github.com/realzhangm/xaux/pkg/x"
)

func TestSubtitleWindowWrapsAndScrollsLines(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	window := newSubtitleWindow()
	if window == nil {
		t.Fatal("could not acquire subtitle task")
	}
	stopped := false
	window.start(func() { stopped = true })

	if window.lines[0].Text != tr("speech.waiting") {
		t.Fatalf("initial subtitle = %q, want waiting hint", window.lines[0].Text)
	}

	window.applyASRResult(&x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 1, Result: "第一句", Interim: true},
	})
	window.applyASRResult(&x.AllResponse{
		Type:   x.TypeSentenceEnd,
		Result: x.RecognizeResult{Index: 1, Result: "第一句话完整。"},
	})
	window.applyASRResult(&x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 2, Result: "第二句", Interim: true},
	})
	if window.lines[0].Text != "第一句话完整。 第二句" || window.lines[1].Text != "" {
		t.Fatalf("short caption lines = %q / %q", window.lines[0].Text, window.lines[1].Text)
	}

	// A long sentence must wrap into lines and scroll so the newest words
	// stay visible; no leading character may be cut off inside a line.
	long := strings.Repeat("字幕内容", 80)
	window.applyASRResult(&x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 2, Result: long, Interim: true},
	})
	if window.lines[0].Text == "" || window.lines[1].Text == "" {
		t.Fatalf("long caption did not fill both lines: %q / %q",
			window.lines[0].Text, window.lines[1].Text)
	}
	full := "第一句话完整。 " + long
	shown := window.lines[0].Text + window.lines[1].Text
	if !strings.HasSuffix(full, shown) {
		t.Fatalf("visible caption is not the tail of the transcript: %q", shown)
	}
	if len([]rune(shown)) >= len([]rune(full)) {
		t.Fatal("long caption did not scroll older lines away")
	}

	window.shutdown()
	<-window.shutdownDone
	if !stopped {
		t.Error("closing the subtitle window did not stop the capture")
	}
	if !acquireVoiceTask(subtitleMenuName) {
		t.Fatal("subtitle task was not released after shutdown")
	}
	releaseVoiceTask(subtitleMenuName)
}

func TestSubtitleTextSizeAdjustment(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	window := newSubtitleWindow()
	if window == nil {
		t.Fatal("could not acquire subtitle task")
	}
	window.start(func() {})
	defer func() {
		window.shutdown()
		<-window.shutdownDone
	}()

	original := window.textSize
	window.adjustTextSize(subtitleTextSizeStep)
	if window.textSize != original+subtitleTextSizeStep {
		t.Fatalf("text size = %v, want %v", window.textSize, original+subtitleTextSizeStep)
	}
	if window.lines[0].TextSize != window.textSize {
		t.Fatalf("caption lines were not re-rendered with the new size")
	}

	for range 10 {
		window.adjustTextSize(subtitleTextSizeStep)
	}
	if window.textSize != subtitleMaxTextSize {
		t.Fatalf("text size = %v, want clamped maximum %v", window.textSize, float32(subtitleMaxTextSize))
	}
	for range 10 {
		window.adjustTextSize(-subtitleTextSizeStep)
	}
	if window.textSize != subtitleMinTextSize {
		t.Fatalf("text size = %v, want clamped minimum %v", window.textSize, float32(subtitleMinTextSize))
	}
	if got := app.Preferences().FloatWithFallback(subtitleTextSizeKey, 0); got != float64(subtitleMinTextSize) {
		t.Fatalf("text size preference = %v, want %v", got, float64(subtitleMinTextSize))
	}
}

func TestWrapSubtitleText(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	if lines := wrapSubtitleText("", subtitleDefaultTextSize, subtitleTextStyle, 300); lines != nil {
		t.Fatalf("empty text produced lines: %v", lines)
	}

	short := "短句"
	if lines := wrapSubtitleText(short, subtitleDefaultTextSize, subtitleTextStyle, 300); len(lines) != 1 || lines[0] != short {
		t.Fatalf("short text was wrapped: %v", lines)
	}

	long := strings.Repeat("标准字幕", 40)
	lines := wrapSubtitleText(long, subtitleDefaultTextSize, subtitleTextStyle, 300)
	if len(lines) < 2 {
		t.Fatalf("long text was not wrapped: %d lines", len(lines))
	}
	for i, line := range lines {
		if width := fyne.MeasureText(line, subtitleDefaultTextSize, subtitleTextStyle).Width; width > 300 {
			t.Fatalf("line %d exceeds the wrap width: %v", i, width)
		}
	}
	if strings.Join(lines, "") != long {
		t.Fatal("wrapping dropped or duplicated characters")
	}
}
