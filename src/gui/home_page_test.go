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

func TestMainMenuLanguageSwitchKeepsSingleQuitAction(t *testing.T) {
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
	window := app.NewWindow("menu test")
	defer window.Close()
	main := &MainWin{app: app, win: window}
	main.bindMainMenu()
	defer main.unsubscribeLocale()

	cases := []struct {
		language string
		menus    []string
		items    [][]string
	}{
		{localeEnglish, []string{"Start", "Voice", "History", "About"}, [][]string{
			{"Settings", "Exit"}, {"Record", "Transcribe", "Interview"}, {"Session history"}, {"Version", "github"},
		}},
		{localeChinese, []string{"开始", "语音", "历史", "关于"}, [][]string{
			{"设置", "退出"}, {"录音", "转写", "面试"}, {"会话历史"}, {"版本显示", "github"},
		}},
		{localeEnglish, []string{"Start", "Voice", "History", "About"}, [][]string{
			{"Settings", "Exit"}, {"Record", "Transcribe", "Interview"}, {"Session history"}, {"Version", "github"},
		}},
	}
	for _, tc := range cases {
		if err := uiLocale.setLanguage(tc.language); err != nil {
			t.Fatal(err)
		}
		menu := window.MainMenu()
		if len(menu.Items) != len(tc.menus) {
			t.Fatalf("%s: got %d top-level menus, want %d", tc.language, len(menu.Items), len(tc.menus))
		}
		for i, expected := range tc.menus {
			group := menu.Items[i]
			if group.Label != expected || len(group.Items) != len(tc.items[i]) {
				t.Fatalf("%s: menu %d = %q with %d items", tc.language, i, group.Label, len(group.Items))
			}
			for j, label := range tc.items[i] {
				if group.Items[j].Label != label {
					t.Errorf("%s: menu %q item %d = %q, want %q", tc.language, expected, j, group.Items[j].Label, label)
				}
			}
		}
		quit := menu.Items[0].Items[len(menu.Items[0].Items)-1]
		if !quit.IsQuit || quit.Action == nil {
			t.Fatalf("%s: final menu item must be an explicit quit item with an action", tc.language)
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

func TestHomeDeviceLabelsFitBothLanguages(t *testing.T) {
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
	setting := newAppSetting()
	panel := setting.makeAudioDevicePanel(nil, nil).(*fyne.Container)
	setting.homeLocale.start()
	defer setting.homeLocale.stop()

	for _, language := range []string{localeChinese, localeEnglish, localeChinese} {
		if err := uiLocale.setLanguage(language); err != nil {
			t.Fatal(err)
		}
		var widths []float32
		for _, index := range []int{2, 4} {
			row := panel.Objects[index].(*fyne.Container)
			wrap := row.Objects[1].(*fyne.Container)
			label := wrap.Objects[0].(*widget.Label)
			width := wrap.MinSize().Width
			if width < label.MinSize().Width {
				t.Errorf("%s label %q needs %.2f width, got %.2f", language, label.Text, label.MinSize().Width, width)
			}
			widths = append(widths, width)
		}
		if widths[0] != widths[1] {
			t.Errorf("%s device label widths are not aligned: %v", language, widths)
		}
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
