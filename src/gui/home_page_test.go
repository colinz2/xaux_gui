package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestMainMenuUsesSimpleTopLevelNavigation(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	menu := newMainMenu()
	wantLabels := []string{"开始", "语音", "历史", "关于"}
	wantItems := [][]string{
		{"设置", "退出"},
		{"录音", "转写", "面试"},
		{sessionHistoryItemName},
		{versionMenuItemName, "github"},
	}
	if len(menu.Items) != len(wantLabels) {
		t.Fatalf("got %d top-level menus, want %d", len(menu.Items), len(wantLabels))
	}
	for index, want := range wantLabels {
		item := menu.Items[index]
		if item.Label != want {
			t.Errorf("menu %d = %q, want %q", index, item.Label, want)
		}
		if len(item.Items) != len(wantItems[index]) {
			t.Errorf("menu %q has %d items, want %d", want, len(item.Items), len(wantItems[index]))
			continue
		}
		for itemIndex, wantItem := range wantItems[index] {
			if got := item.Items[itemIndex].Label; got != wantItem {
				t.Errorf("menu %q item %d = %q, want %q", want, itemIndex, got, wantItem)
			}
		}
	}
}

func TestHomePageContainsOnlyAudioPanel(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	audioPanel := widget.NewLabel("音频设备")
	page := newHomePage(audioPanel)
	padding, ok := page.(*fyne.Container)
	if !ok || len(padding.Objects) != 1 {
		t.Fatal("home page should have one padded content area")
	}
	content, ok := padding.Objects[0].(*fyne.Container)
	if !ok || len(content.Objects) != 2 {
		t.Fatal("home page should contain only a top gap and audio card")
	}
	gap, ok := content.Objects[0].(*canvas.Rectangle)
	if !ok || gap.MinSize().Height != 4 {
		t.Fatal("home page should leave only a small gap below the menu")
	}
	card, ok := content.Objects[1].(*widget.Card)
	if !ok || card.Content != audioPanel {
		t.Fatal("home page content is not presented in an audio device card")
	}
}

func TestHomePageAudioControlsRemainInteractive(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	setting := newAppSetting()
	panel := setting.makeAudioDevicePanel([]string{"Speaker"}, []string{"Microphone"})
	window := app.NewWindow("首页")
	defer window.Close()
	window.SetContent(newHomePage(panel))
	window.Resize(homeWindowSize(window.Content()))
	window.Show()

	devicePanel := panel.(*fyne.Container)
	speakerCheck := findCheck(devicePanel.Objects[2])
	if speakerCheck == nil {
		t.Fatal("speaker row does not contain a checkbox")
	}
	fynetest.Tap(speakerCheck)
	if setting.speakerSelected {
		t.Fatal("speaker checkbox did not respond to a tap")
	}
}

func findCheck(object fyne.CanvasObject) *widget.Check {
	if check, ok := object.(*widget.Check); ok {
		return check
	}
	container, ok := object.(*fyne.Container)
	if !ok {
		return nil
	}
	for _, child := range container.Objects {
		if check := findCheck(child); check != nil {
			return check
		}
	}
	return nil
}
