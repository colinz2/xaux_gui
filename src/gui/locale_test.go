package gui

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"xaux_gui/pkg/sound_cap"
)

func TestTranslationBundlesHaveMatchingKeys(t *testing.T) {
	var bundles []map[string]json.RawMessage
	for _, path := range []string{"translations/zh-CN.json", "translations/en.json"} {
		data, err := translationFiles.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]json.RawMessage
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatal(err)
		}
		bundles = append(bundles, messages)
	}
	for key := range bundles[0] {
		if _, ok := bundles[1][key]; !ok {
			t.Errorf("English translation missing for %q", key)
		}
	}
	for key := range bundles[1] {
		if _, ok := bundles[0][key]; !ok {
			t.Errorf("Chinese translation missing for %q", key)
		}
	}
	placeholder := regexp.MustCompile(`\{\{\s*\.([A-Za-z][A-Za-z0-9_]*)\s*\}\}`)
	for key, chinese := range bundles[0] {
		english, ok := bundles[1][key]
		if !ok {
			continue
		}
		var zhText, enText string
		if err := json.Unmarshal(chinese, &zhText); err != nil {
			t.Errorf("Chinese message %q is not a string: %v", key, err)
			continue
		}
		if err := json.Unmarshal(english, &enText); err != nil {
			t.Errorf("English message %q is not a string: %v", key, err)
			continue
		}
		parameters := func(text string) map[string]bool {
			result := make(map[string]bool)
			for _, match := range placeholder.FindAllStringSubmatch(text, -1) {
				result[match[1]] = true
			}
			return result
		}
		if !reflect.DeepEqual(parameters(zhText), parameters(enText)) {
			t.Errorf("message %q has different template parameters", key)
		}
	}
}

func TestLocaleManagerSwitchAndSubscriptions(t *testing.T) {
	manager, err := newLocaleManager()
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	unsubscribe := manager.subscribe(func() {
		values = append(values, manager.translate("home.audio_devices", nil))
	})
	if got := manager.translate("home.audio_devices", nil); got != "音频设备" {
		t.Fatalf("default translation = %q", got)
	}
	if err := manager.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if err := manager.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if err := manager.setLanguage("unsupported"); err == nil {
		t.Fatal("unsupported locale was accepted")
	}
	unsubscribe()
	if err := manager.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values, []string{"Audio devices"}) {
		t.Fatalf("language change notifications = %q", values)
	}
}

func TestLocaleBindingsUpdateCurrentStateAndPreserveUserText(t *testing.T) {
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
	status := widget.NewLabel("")
	answer := widget.NewLabel("")
	button := widget.NewButton("", nil)
	bindings.setLabel(status, "interview.generating", nil)
	bindings.setLabel(answer, "interview.thinking_help", nil)
	bindings.setButton(button, "speech.pause", nil)
	bindings.start()
	defer bindings.stop()

	bindings.forgetLabel(answer)
	answer.SetText("用户输入和 AI 输出")
	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if status.Text != "Generating" || button.Text != "Pause" || answer.Text != "用户输入和 AI 输出" {
		t.Fatalf("English state: status=%q button=%q answer=%q", status.Text, button.Text, answer.Text)
	}
	bindings.setLabel(status, "common.paused", nil)
	bindings.setButton(button, "speech.resume", nil)
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	if status.Text != "已暂停" || button.Text != "继续" || answer.Text != "用户输入和 AI 输出" {
		t.Fatalf("Chinese state: status=%q button=%q answer=%q", status.Text, button.Text, answer.Text)
	}
}

func TestRecorderLanguageSwitchKeepsInputAndSourceState(t *testing.T) {
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
	win := &RecorderWin{
		saveDir:       t.TempDir(),
		sessionName:   widget.NewEntry(),
		durationLabel: widget.NewLabel(""),
		errorLabel:    widget.NewLabel(""),
		locale:        &localeBindings{},
	}
	win.sources = []*recordSource{
		newRecordSource(win, "麦克风", "device", sound_cap.CaptureMicrophone, true, true),
		newRecordSource(win, "扬声器", "", sound_cap.CaptureSpeaker, false, false),
	}
	win.makeContent()
	win.sessionName.SetText("待保存的会话")
	win.sources[0].setStatus("record.active")
	win.locale.start()
	defer win.locale.stop()

	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if win.startButton.Text != "Start recording" || win.sources[0].status.Text != "Recording" ||
		win.sources[1].status.Text != "Not selected" || win.sessionName.Text != "待保存的会话" {
		t.Fatalf("language switch lost recorder state: %q, %q, %q, %q",
			win.startButton.Text, win.sources[0].status.Text, win.sources[1].status.Text, win.sessionName.Text)
	}
	win.sources[0].setStatus("common.stopped")
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	if win.sources[0].status.Text != "已停止" || win.sessionName.Text != "待保存的会话" {
		t.Fatal("recorder state was not preserved after switching back")
	}
}

func TestHistoryLanguageSwitchKeepsSelectedSessionAndReview(t *testing.T) {
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
	setting.sessionRootDir = t.TempDir()
	win := &sessionHistoryWin{setting: setting, locale: &localeBindings{}, window: app.NewWindow("history")}
	win.buildContent()
	defer win.window.Close()
	defer win.locale.stop()
	defer func() { win.detailsLocale.stop() }()
	win.locale.start()
	info := SessionInfo{Name: "用户会话", Directory: setting.sessionRootDir, Kind: SessionKindInterview, Status: SessionStatusRunning}
	win.showSession(info)
	win.setReviewText("用户的复盘内容")
	win.historyTabs.Select(win.reviewTab)
	if err := uiLocale.setLanguage(localeEnglish); err != nil {
		t.Fatal(err)
	}
	if win.selected == nil || win.selected.Name != info.Name || win.name.Text != info.Name ||
		win.review.Text != "用户的复盘内容" || win.historyTabs.Selected() != win.reviewTab {
		t.Fatal("language switch changed the selected session, review, or selected tab")
	}
	if win.reviewTab.Text != "AI review" || win.deleteButton.Text != "Delete session" {
		t.Fatalf("history controls not translated: %q, %q", win.reviewTab.Text, win.deleteButton.Text)
	}
}

func TestLocalizedMessageErrorKeepsCauseAndDiagnostic(t *testing.T) {
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := uiLocale.setLanguage(localeChinese); err != nil {
			t.Error(err)
		}
	})
	cause := errors.New("detail")
	err := localizedMessageError("model.key_required", nil, "API key is required", cause)
	if err.Error() != "请填写 API Key：detail" || !errors.Is(err, cause) {
		t.Fatalf("Chinese error: %v", err)
	}
	if switchErr := uiLocale.setLanguage(localeEnglish); switchErr != nil {
		t.Fatal(switchErr)
	}
	if err.Error() != "Enter an API key: detail" || diagnosticError(err) != "API key is required: detail" {
		t.Fatalf("English error or diagnostic: %v / %s", err, diagnosticError(err))
	}
}
func TestSettingsLanguageSwitchKeepsUnsavedFields(t *testing.T) {
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
	setting.initialized = true
	setting.sessionRootDir = t.TempDir()
	if err := setting.openConfigurationWindow(); err != nil {
		t.Fatal(err)
	}
	defer setting.closeWindows()
	setting.modelAPIKeyEntry.SetText("unsaved-test-key")
	setting.modelNameEntry.SetText("unsaved-model")
	setting.modelPromptEntry.SetText("未保存的提示词")
	scroll := setting.configurationWindow.Content().(*container.Scroll)
	padded := scroll.Content.(*fyne.Container)
	panels := padded.Objects[0].(*fyne.Container)
	row := panels.Objects[0].(*fyne.Container)
	var languageSelect *widget.Select
	for _, object := range row.Objects {
		if selectWidget, ok := object.(*widget.Select); ok {
			languageSelect = selectWidget
		}
	}
	if languageSelect == nil {
		t.Fatal("language selector not found")
	}
	languageSelect.SetSelected("English")
	if uiLocale.language() != localeEnglish || app.Preferences().String(localePreference) != localeEnglish {
		t.Fatal("language selection was not applied and persisted")
	}
	if setting.modelAPIKeyEntry.Text != "unsaved-test-key" ||
		setting.modelNameEntry.Text != "unsaved-model" ||
		setting.modelPromptEntry.Text != "未保存的提示词" || !setting.modelConfigDirty {
		t.Fatal("language switch lost unsaved model settings")
	}
	if err := uiLocale.setLanguage(localeChinese); err != nil {
		t.Fatal(err)
	}
	restoreUILanguage(app.Preferences())
	if uiLocale.language() != localeEnglish {
		t.Fatal("saved language was not restored from preferences")
	}
	languageSelect.SetSelected("简体中文")
	if uiLocale.language() != localeChinese || app.Preferences().String(localePreference) != localeChinese {
		t.Fatal("switching back did not persist the Chinese language")
	}
}
