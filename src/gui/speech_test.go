package gui

import (
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
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

	speech := &speechRTTrans{
		transcript:   container.NewVBox(),
		latestButton: widget.NewButton("回到最新", nil),
		followLatest: true,
	}
	speech.scroll = container.NewVScroll(speech.transcript)
	speech.running.Store(true)
	capture := &asrSoundCap{resultIndex: -1, writeListIndex: -1}

	speech.applyASRResult(capture, &x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 0, Result: "par", Interim: true},
	})
	if len(speech.listItems) != 1 || speech.listItems[0].content != "par" {
		t.Fatalf("first zero-index result was not appended: %+v", speech.listItems)
	}
	if speech.rows[0].content.Wrapping != fyne.TextWrapBreak || speech.rows[0].status.Text != "识别中" {
		t.Fatalf("partial result was not rendered as a wrapped interim row: %+v", speech.rows[0])
	}

	speech.applyASRResult(capture, &x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 0, Result: "partial", Interim: true},
	})
	if len(speech.listItems) != 1 || speech.listItems[0].content != "partial" {
		t.Fatalf("successive interim result did not update the existing row: %+v", speech.listItems)
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

func TestASRSoundCapFiltersDuplicateResultsAndResetsAtSentenceStart(t *testing.T) {
	capture := &asrSoundCap{}
	events := []*x.AllResponse{
		{Type: x.TypeRecognizing, Result: x.RecognizeResult{Index: 0, Result: "你", Interim: true}},
		{Type: x.TypeRecognizing, Result: x.RecognizeResult{Index: 0, Result: "你", Interim: true}},
		{Type: x.TypeRecognizing, Result: x.RecognizeResult{Index: 0, Result: "你好", Interim: true}},
		{Type: x.TypeSentenceEnd, Result: x.RecognizeResult{Index: 0, Result: "你好"}},
		{Type: x.TypeSentenceEnd, Result: x.RecognizeResult{Index: 0, Result: "你好"}},
		{Type: x.TypeSentenceStart},
		{Type: x.TypeRecognizing, Result: x.RecognizeResult{Index: 0, Result: "新句", Interim: true}},
	}
	wantAccepted := []bool{true, false, true, true, false, true, true}

	for i, event := range events {
		accepted, _ := capture.acceptASRResult(event)
		if accepted != wantAccepted[i] {
			t.Errorf("event %d accepted = %t, want %t", i, accepted, wantAccepted[i])
		}
	}
}

func TestASRFinalityUsesProtocolEventType(t *testing.T) {
	tests := []struct {
		name     string
		response *x.AllResponse
		want     bool
	}{
		{
			name: "recognizing remains interim even if interim flag is unset",
			response: &x.AllResponse{
				Type:   x.TypeRecognizing,
				Result: x.RecognizeResult{Result: "识别中"},
			},
		},
		{
			name: "sentence end is final even if interim flag is set",
			response: &x.AllResponse{
				Type:   x.TypeSentenceEnd,
				Result: x.RecognizeResult{Result: "已完成", Interim: true},
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := asrResponseIsFinal(test.response); got != test.want {
				t.Fatalf("asrResponseIsFinal() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestSpeechRTTransKeepsConcurrentSourcesIndependent(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	speech := &speechRTTrans{
		transcript:   container.NewVBox(),
		latestButton: widget.NewButton("回到最新", nil),
		followLatest: true,
	}
	speech.scroll = container.NewVScroll(speech.transcript)
	speech.running.Store(true)
	speaker := &asrSoundCap{
		deviceName:     "扬声器",
		resultIndex:    -1,
		writeListIndex: -1,
	}
	microphone := &asrSoundCap{
		deviceName:     "麦克风",
		isMicrophone:   true,
		resultIndex:    -1,
		writeListIndex: -1,
	}

	speech.applyASRResult(speaker, &x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 0, Result: "扬声器未完成", Interim: true},
	})
	speech.applyASRResult(microphone, &x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 0, Result: "麦克风识别中", Interim: true},
	})
	speech.applyASRResult(microphone, &x.AllResponse{
		Type:   x.TypeSentenceEnd,
		Result: x.RecognizeResult{Index: 0, Result: "麦克风已完成"},
	})

	if len(speech.listItems) != 2 {
		t.Fatalf("interleaved streams produced %d rows, want 2", len(speech.listItems))
	}
	if item := speech.listItems[speaker.writeListIndex]; item.content != "扬声器未完成" || item.final {
		t.Fatalf("microphone finalization changed speaker interim row: %+v", item)
	}
	if item := speech.listItems[microphone.writeListIndex]; item.content != "麦克风已完成" || !item.final {
		t.Fatalf("microphone row was not finalized in place: %+v", item)
	}
	if speech.rows[speaker.writeListIndex].status.Text != "识别中" ||
		speech.rows[microphone.writeListIndex].status.Text != "" {
		t.Fatalf("unexpected independent row states: speaker=%q microphone=%q",
			speech.rows[speaker.writeListIndex].status.Text,
			speech.rows[microphone.writeListIndex].status.Text)
	}

	speech.applyASRResult(speaker, &x.AllResponse{
		Type:   x.TypeSentenceEnd,
		Result: x.RecognizeResult{Index: 0, Result: "扬声器已完成"},
	})
	if item := speech.listItems[speaker.writeListIndex]; item.content != "扬声器已完成" || !item.final {
		t.Fatalf("speaker row was not finalized in place: %+v", item)
	}
	if speech.listItems[0].deviceName != "扬声器" || speech.listItems[1].deviceName != "麦克风" {
		t.Fatalf("rows moved while the streams finalized: %+v", speech.listItems)
	}
}

func TestTranscriptNearBottom(t *testing.T) {
	tests := []struct {
		name       string
		content    float32
		viewport   float32
		offset     float32
		wantFollow bool
	}{
		{name: "short content", content: 200, viewport: 400, wantFollow: true},
		{name: "at bottom", content: 800, viewport: 400, offset: 400, wantFollow: true},
		{name: "near bottom", content: 800, viewport: 400, offset: 370, wantFollow: true},
		{name: "reading earlier text", content: 800, viewport: 400, offset: 200, wantFollow: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := transcriptNearBottom(test.content, test.viewport, test.offset); got != test.wantFollow {
				t.Fatalf("transcriptNearBottom() = %v, want %v", got, test.wantFollow)
			}
		})
	}
}

func TestSpeechShutdownOnce(t *testing.T) {
	resetVoiceTasksForTest()
	window := newSpeechRTTrans()
	calls := make(chan struct{}, 2)
	window.onClosed = func() { calls <- struct{}{} }
	window.shutdown()
	window.shutdown()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("shutdown callback did not run")
	}
	window.release()
	select {
	case <-calls:
		t.Fatal("shutdown callback ran more than once")
	case <-time.After(20 * time.Millisecond):
	}
	if next := newSpeechRTTrans(); next == nil {
		t.Fatal("shutdown did not release the transcription window")
	} else {
		next.release()
	}
}

func TestSpeechWindowSingletonRelease(t *testing.T) {
	resetVoiceTasksForTest()
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
