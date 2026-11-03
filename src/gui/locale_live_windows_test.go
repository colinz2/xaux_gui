package gui

import (
	"testing"

	fynetest "fyne.io/fyne/v2/test"
	"github.com/realzhangm/xaux/pkg/x"
)

func TestTranscriptionWindowLanguageSwitchPreservesTranscript(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := uiLocale.setLanguage(localeChinese); err != nil {
			t.Error(err)
		}
	})
	window := newSpeechRTTrans()
	if window == nil {
		t.Fatal("could not acquire transcription task")
	}
	window.start(nil, nil, nil)
	defer func() {
		window.running.Store(false)
		window.duration.Stop()
		window.window.SetCloseIntercept(nil)
		window.window.Close()
		window.release()
	}()
	capture := &asrSoundCap{deviceName: "device", resultIndex: -1, writeListIndex: -1}
	window.applyASRResult(capture, &x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 1, Result: "识别内容", Interim: true},
	})
	window.followLatest = false
	window.updateTranscriptFollow()
	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if window.window.Title() != "Transcription" || window.status.Text != "Listening" ||
		window.rows[0].status.Text != "Recognizing" || window.rows[0].content.Text != "识别内容" ||
		window.followLatest {
		t.Fatal("switching language changed the running transcription or its contents")
	}
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	if window.rows[0].status.Text != "识别中" || window.rows[0].content.Text != "识别内容" {
		t.Fatal("switching back did not restore the transcript status")
	}
}

func TestInterviewWindowLanguageSwitchPreservesConversation(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := uiLocale.setLanguage(localeChinese); err != nil {
			t.Error(err)
		}
	})
	session, err := CreateSession(t.TempDir(), "interview", SessionKindInterview)
	if err != nil {
		t.Fatal(err)
	}
	window := newInterviewAssistant(nil, modelConfig{})
	if window == nil {
		t.Fatal("could not acquire interview task")
	}
	window.session = session
	window.buildContent()
	window.running.Store(true)
	defer func() {
		window.running.Store(false)
		window.cancel()
		window.duration.Stop()
		window.window.SetCloseIntercept(nil)
		window.window.Close()
		window.release()
	}()
	capture := &asrSoundCap{deviceName: "device", resultIndex: -1, writeListIndex: -1}
	window.applyASRResult(capture, &x.AllResponse{
		Type:   x.TypeRecognizing,
		Result: x.RecognizeResult{Index: 1, Result: "面试内容", Interim: true},
	})
	window.setListeningStatus()
	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if window.window.Title() != "Interview" || window.status.Text != "Listening" ||
		window.rows[0].status.Text != "Recognizing" || window.rows[0].content.Text != "面试内容" {
		t.Fatal("switching language changed the running interview or its contents")
	}
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	if window.rows[0].status.Text != "识别中" || window.rows[0].content.Text != "面试内容" {
		t.Fatal("switching back did not restore the conversation status")
	}
}
