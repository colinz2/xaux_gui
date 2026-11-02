package gui

import (
	"path/filepath"
	"regexp"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/data/binding"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/x"
)

func TestAudioFilePath(t *testing.T) {
	dir := t.TempDir()
	microphonePath := audioFilePath(dir, true)
	speakerPath := audioFilePath(dir, false)
	if microphonePath == speakerPath {
		t.Fatal("expected unique paths for simultaneous recordings")
	}
	if filepath.Dir(microphonePath) != dir || filepath.Dir(speakerPath) != dir {
		t.Fatal("recording was written outside the requested directory")
	}
	pattern := regexp.MustCompile(`^\d{8}-\d{6}_\d{6}_(microphone|speaker)\.wav$`)
	for _, name := range []string{filepath.Base(microphonePath), filepath.Base(speakerPath)} {
		if !pattern.MatchString(name) {
			t.Fatalf("unexpected audio file name %q", name)
		}
	}
}

func TestSpeechRTTransApplyASRResult(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	speech := &speechRTTrans{binding: binding.NewUntypedList()}
	speech.list = widget.NewList(
		func() int { return len(speech.listItems) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(widget.ListItemID, fyne.CanvasObject) {},
	)
	speech.running.Store(true)
	capture := &asrSoundCap{resultIndex: -1, writeListIndex: -1}

	speech.applyASRResult(capture, &x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 0, Result: "partial", Interim: true},
	})
	if len(speech.listItems) != 1 || speech.listItems[0].content != "partial" {
		t.Fatalf("first zero-index result was not appended: %+v", speech.listItems)
	}

	speech.applyASRResult(capture, &x.AllResponse{
		Type:   x.TypeSentenceEnd,
		Result: x.RecognizeResult{Index: 0, Result: "final"},
	})
	if len(speech.listItems) != 1 {
		t.Fatalf("same result index created %d items", len(speech.listItems))
	}
	if item := speech.listItems[0]; item.content != "final" || !item.final {
		t.Fatalf("result was not updated: %+v", item)
	}

	speech.running.Store(false)
	speech.applyASRResult(capture, &x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 1, Result: "ignored", Interim: true},
	})
	if len(speech.listItems) != 1 {
		t.Fatal("closed transcription accepted a result")
	}
}

func TestSpeechShutdownOnce(t *testing.T) {
	speechWindowOpen.Store(false)
	window := newSpeechRTTrans()
	calls := 0
	window.onClosed = func() { calls++ }
	window.shutdown()
	window.shutdown()
	if calls != 1 {
		t.Fatalf("shutdown callback ran %d times, want 1", calls)
	}
	if next := newSpeechRTTrans(); next == nil {
		t.Fatal("shutdown did not release the transcription window")
	} else {
		next.release()
	}
}

func TestSpeechWindowSingletonRelease(t *testing.T) {
	speechWindowOpen.Store(false)
	first := newSpeechRTTrans()
	if first == nil {
		t.Fatal("failed to acquire transcription window")
	}
	if second := newSpeechRTTrans(); second != nil {
		t.Fatal("opened a second transcription window")
	}
	first.release()
	first.release()
	if next := newSpeechRTTrans(); next == nil {
		t.Fatal("failed to reopen transcription window after release")
	} else {
		next.release()
	}
}
