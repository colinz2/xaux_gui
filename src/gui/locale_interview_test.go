package gui

import (
	"strings"
	"testing"

	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestGeneratingAnswerIsSavedAfterLanguageSwitch(t *testing.T) {
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
	bindings := &localeBindings{}
	answer := &interviewAnswer{
		question:   "问题",
		content:    "未完成的回答",
		generating: true,
		status:     widget.NewLabel(""),
	}
	bindings.setLabel(answer.status, "interview.generating", nil)
	bindings.start()
	defer bindings.stop()
	assistant := &interviewAssistant{locale: bindings, answers: []*interviewAnswer{answer}}
	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if answer.status.Text != "Generating" {
		t.Fatalf("status = %q", answer.status.Text)
	}
	assistant.saveGeneratingAnswers()
	if !answer.saved || answer.generating || answer.status.Text != "Interrupted" ||
		len(assistant.savedEntries) != 1 || !strings.Contains(assistant.savedEntries[0].Content, answer.content) {
		t.Fatal("an interrupted answer was not saved after switching languages")
	}
}
