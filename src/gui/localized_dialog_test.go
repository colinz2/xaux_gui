package gui

import (
	"errors"
	"os"
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func dialogContents(parent fyne.Window) (map[string]*widget.Button, map[string]bool) {
	buttons := make(map[string]*widget.Button)
	labels := make(map[string]bool)
	seen := make(map[fyne.CanvasObject]bool)
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		if object == nil || seen[object] {
			return
		}
		seen[object] = true
		switch item := object.(type) {
		case *widget.Button:
			buttons[item.Text] = item
		case *widget.Label:
			labels[item.Text] = true
		}
		if group, ok := object.(*fyne.Container); ok {
			for _, child := range group.Objects {
				visit(child)
			}
		} else if item, ok := object.(fyne.Widget); ok {
			for _, child := range fynetest.WidgetRenderer(item).Objects() {
				visit(child)
			}
		}
	}
	for _, overlay := range parent.Canvas().Overlays().List() {
		visit(overlay)
	}
	return buttons, labels
}

func TestLocalizedDialogs(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	parent := app.NewWindow("test")
	defer parent.Close()
	parent.Resize(fyne.NewSize(800, 600))
	t.Cleanup(func() {
		if err := uiLocale.setLanguage(localeChinese); err != nil {
			t.Error(err)
		}
	})

	for _, tc := range []struct {
		language, yes, no, ok, errorTitle string
	}{
		{localeChinese, "是", "否", "确定", "错误"},
		{localeEnglish, "Yes", "No", "OK", "Error"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			if err := uiLocale.setLanguage(tc.language); err != nil {
				t.Fatal(err)
			}
			var responses []bool
			for _, choice := range []string{tc.no, tc.yes} {
				showLocalizedConfirm("Title", "Message", func(answer bool) {
					responses = append(responses, answer)
				}, parent)
				buttons, _ := dialogContents(parent)
				if buttons[tc.yes] == nil || buttons[tc.no] == nil {
					t.Fatalf("confirmation buttons = %v", buttons)
				}
				fynetest.Tap(buttons[choice])
			}
			if len(responses) != 2 || responses[0] || !responses[1] {
				t.Fatalf("confirmation responses = %v", responses)
			}

			showLocalizedInformation("Title", "Message", parent)
			buttons, _ := dialogContents(parent)
			if buttons[tc.ok] == nil {
				t.Fatalf("information buttons = %v", buttons)
			}
			fynetest.Tap(buttons[tc.ok])

			showLocalizedError(errors.New("example failure"), parent)
			buttons, labels := dialogContents(parent)
			if buttons[tc.ok] == nil || !labels[tc.errorTitle] || !labels["Example failure"] {
				t.Fatalf("error dialog buttons = %v, labels = %v", buttons, labels)
			}
			fynetest.Tap(buttons[tc.ok])
		})
	}
}

func TestHistoryDeleteConfirmation(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := uiLocale.setLanguage(localeChinese); err != nil {
			t.Error(err)
		}
	})
	root := t.TempDir()
	session, err := CreateSession(root, "delete-me", SessionKindRecording)
	if err != nil {
		t.Fatal(err)
	}
	info := session.Snapshot()
	window := app.NewWindow("history")
	defer window.Close()
	history := &sessionHistoryWin{
		window: window, setting: &appSetting{Setting: Setting{sessionRootDir: root}},
		locale: &localeBindings{}, selected: &info,
	}
	history.buildContent()
	defer history.detailsLocale.stop()

	history.confirmDeleteSession()
	buttons, _ := dialogContents(window)
	if buttons["No"] == nil || buttons["Yes"] == nil {
		t.Fatalf("delete confirmation buttons = %v", buttons)
	}
	fynetest.Tap(buttons["No"])
	if _, err := os.Stat(info.Directory); err != nil {
		t.Fatalf("cancel removed session: %v", err)
	}

	history.confirmDeleteSession()
	buttons, _ = dialogContents(window)
	fynetest.Tap(buttons["Yes"])
	if _, err := os.Stat(info.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("confirmed session still exists: %v", err)
	}
}
