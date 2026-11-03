package gui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestReferenceErrorRelocalizesWithoutLosingCause(t *testing.T) {
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := uiLocale.setLanguage(localeChinese); err != nil {
			t.Error(err)
		}
	})
	_, err := readInterviewReference(filepath.Join(t.TempDir(), "missing.txt"), &localizedText{key: "reference.job"})
	if err == nil || !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "打开职位描述失败") {
		t.Fatalf("Chinese error or cause incorrect: %v", err)
	}
	if switchErr := uiLocale.setLanguage(localeEnglish); switchErr != nil {
		t.Fatal(switchErr)
	}
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "Failed to open Job description") {
		t.Fatalf("English error or cause incorrect: %v", err)
	}
}

func TestReferencePickerLanguageSwitch(t *testing.T) {
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
	parent := app.NewWindow("settings")
	defer parent.Close()
	openReferencePicker(parent, referencePanelConfig{label: "settings.resume"}, func(string) {})
	var picker fyne.Window
	for _, window := range app.Driver().AllWindows() {
		if window.Title() == "选择简历" {
			picker = window
			break
		}
	}
	if picker == nil {
		t.Fatal("reference picker was not opened")
	}
	defer picker.Close()
	if picker.Title() != "选择简历" {
		t.Fatalf("Chinese picker title = %q", picker.Title())
	}
	content := picker.Content().(*fyne.Container)
	hint := content.Objects[0].(*widget.Label)
	if hint.Text != "请选择简历" {
		t.Fatalf("Chinese picker hint = %q", hint.Text)
	}
	var openButton, cancelButton *widget.Button
	seen := make(map[fyne.CanvasObject]bool)
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		if object == nil || seen[object] {
			return
		}
		seen[object] = true
		if button, ok := object.(*widget.Button); ok {
			switch button.Text {
			case "打开":
				openButton = button
			case "取消":
				cancelButton = button
			}
		}
		if group, ok := object.(*fyne.Container); ok {
			for _, child := range group.Objects {
				visit(child)
			}
		} else if widgetObject, ok := object.(fyne.Widget); ok {
			for _, child := range fynetest.WidgetRenderer(widgetObject).Objects() {
				visit(child)
			}
		}
	}
	for _, overlay := range picker.Canvas().Overlays().List() {
		visit(overlay)
	}
	if openButton == nil || cancelButton == nil {
		t.Fatal("reference picker buttons not found")
	}
	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if picker.Title() != "Choose Resume" || hint.Text != "Choose Resume" ||
		openButton.Text != "Open" || cancelButton.Text != "Cancel" {
		t.Fatalf("English picker title/hint/buttons = %q / %q / %q / %q",
			picker.Title(), hint.Text, openButton.Text, cancelButton.Text)
	}
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	if picker.Title() != "选择简历" || hint.Text != "请选择简历" ||
		openButton.Text != "打开" || cancelButton.Text != "取消" {
		t.Fatalf("picker title/hint/buttons after switching back = %q / %q / %q / %q",
			picker.Title(), hint.Text, openButton.Text, cancelButton.Text)
	}
}

func TestKnowledgeFolderPickerLanguageSwitch(t *testing.T) {
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
	parent := app.NewWindow("settings")
	defer parent.Close()
	openReferencePicker(parent, referencePanelConfig{label: "settings.knowledge", directory: true}, func(string) {})
	var picker fyne.Window
	for _, window := range app.Driver().AllWindows() {
		if window.Title() == "选择面试知识" {
			picker = window
			break
		}
	}
	if picker == nil {
		t.Fatal("knowledge folder picker was not opened")
	}
	defer picker.Close()
	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if picker.Title() != "Choose Interview knowledge" {
		t.Fatalf("English knowledge picker title = %q", picker.Title())
	}
}
func TestHistoryReferenceErrorLanguageSwitch(t *testing.T) {
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
	window := app.NewWindow("history")
	defer window.Close()
	win := &sessionHistoryWin{
		window: window, setting: newAppSetting(), locale: &localeBindings{},
	}
	win.buildContent()
	win.locale.start()
	defer win.locale.stop()
	defer func() { win.detailsLocale.stop() }()
	win.showSession(SessionInfo{
		Name: "test", Directory: t.TempDir(), Kind: SessionKindInterview,
		Status: SessionStatusStopped, JobDescriptionFile: "missing.txt", Error: "用户原始错误",
	})
	if !strings.Contains(win.detailError.Text, "打开职位描述失败") {
		t.Fatalf("Chinese history error = %q", win.detailError.Text)
	}
	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(win.detailError.Text, "Failed to open Job description") ||
		!strings.Contains(win.detailError.Text, "用户原始错误") {
		t.Fatalf("English history error = %q", win.detailError.Text)
	}
}
