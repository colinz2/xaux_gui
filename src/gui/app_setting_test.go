package gui

import (
	"strings"
	"testing"

	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestAudioDevicePanelContainsOnlyDeviceControls(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	setting := newAppSetting()
	panel := setting.makeAudioDevicePanel([]string{"Speaker"}, []string{"Microphone"})
	content, ok := panel.(*fyne.Container)
	if !ok {
		t.Fatalf("audio device panel has type %T, want *fyne.Container", panel)
	}
	if len(content.Objects) != 5 {
		t.Fatalf("audio device panel has %d children, want heading, two devices, and separators", len(content.Objects))
	}
	heading, ok := content.Objects[0].(*canvas.Text)
	if !ok || heading.Text != "音频设备" {
		t.Fatal("audio device panel does not start with the expected heading")
	}
	if heading.TextSize <= theme.TextSize() || !heading.TextStyle.Bold {
		t.Fatal("audio device heading should be larger and bolder than normal text")
	}
}

func TestConfigurationWindowContainsPersistentInterviewReferences(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	root := t.TempDir()
	setting := newAppSetting()
	setting.initialized = true
	setting.sessionRootDir = root
	setting.loadInterviewReferencePreferences()
	if err := setting.openConfigurationWindow(); err != nil {
		t.Fatal(err)
	}
	defer setting.closeWindows()

	scroll, ok := setting.configurationWindow.Content().(*container.Scroll)
	if !ok {
		t.Fatal("settings content should scroll when it exceeds the window")
	}
	padded := scroll.Content.(*fyne.Container)
	panels := padded.Objects[0].(*fyne.Container)
	if len(panels.Objects) != 4 {
		t.Fatalf("settings has %d panels, want language, session, model and interview", len(panels.Objects))
	}
	referenceCard, ok := panels.Objects[3].(*widget.Card)
	if !ok || referenceCard.Title != "面试资料" {
		t.Fatal("interview references are missing from settings")
	}
	references := referenceCard.Content.(*fyne.Container)
	if len(references.Objects) != 3 {
		t.Fatal("settings should contain both resume and knowledge controls")
	}
	resume := references.Objects[0].(*fyne.Container)
	row := resume.Objects[0].(*fyne.Container)
	check := findCheck(row)
	if check == nil {
		t.Fatal("resume checkbox is missing")
	}
	var entry *widget.Entry
	for _, object := range row.Objects {
		if value, ok := object.(*widget.Entry); ok {
			entry = value
		}
	}
	if entry == nil {
		t.Fatal("resume path entry is missing")
	}
	check.SetChecked(true)
	entry.SetText("resume.md")
	if !setting.resumeEnabled || setting.resumePath != "resume.md" {
		t.Fatal("settings controls did not update resume configuration")
	}
	expectedSize := configurationWindowSize(padded)
	if got := setting.configurationWindow.Canvas().Size(); got != expectedSize {
		t.Fatalf("settings canvas size = %v, want %v", got, expectedSize)
	}
	if padded.MinSize().Height > expectedSize.Height {
		scroll.ScrollToBottom()
		if scroll.Offset.Y <= 0 {
			t.Fatal("settings cannot scroll to the interview reference controls")
		}
		knowledge := references.Objects[2]
		position := app.Driver().AbsolutePositionForObject(knowledge)
		if position.Y+knowledge.Size().Height > scroll.Size().Height+1 {
			t.Fatalf("knowledge controls clipped after scrolling: position=%v size=%v viewport=%v",
				position, knowledge.Size(), scroll.Size())
		}
	}
	setting.closeConfigurationWindow()

	restored := newAppSetting()
	restored.initialized = true
	restored.sessionRootDir = root
	restored.loadInterviewReferencePreferences()
	if !restored.resumeEnabled || restored.resumePath != "resume.md" {
		t.Fatal("resume preferences were not restored after closing settings")
	}
	if err := restored.openConfigurationWindow(); err != nil {
		t.Fatal(err)
	}
	defer restored.closeWindows()
	restoredScroll := restored.configurationWindow.Content().(*container.Scroll)
	restoredPadded := restoredScroll.Content.(*fyne.Container)
	restoredPanels := restoredPadded.Objects[0].(*fyne.Container)
	restoredCard := restoredPanels.Objects[3].(*widget.Card)
	restoredReferences := restoredCard.Content.(*fyne.Container)
	restoredResume := restoredReferences.Objects[0].(*fyne.Container)
	restoredRow := restoredResume.Objects[0].(*fyne.Container)
	restoredCheck := findCheck(restoredRow)
	var restoredEntry *widget.Entry
	for _, object := range restoredRow.Objects {
		if value, ok := object.(*widget.Entry); ok {
			restoredEntry = value
		}
	}
	if restoredCheck == nil || !restoredCheck.Checked || restoredEntry == nil ||
		restoredEntry.Text != "resume.md" || restoredEntry.Disabled() {
		t.Fatal("settings did not display the saved resume configuration")
	}
}

func TestConfigurationFieldsStayAligned(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	t.Cleanup(func() {
		if err := uiLocale.setLanguage(localeChinese); err != nil {
			t.Error(err)
		}
	})
	for _, currentLocale := range []string{localeChinese, localeEnglish} {
		if err := uiLocale.setLanguage(currentLocale); err != nil {
			t.Fatal(err)
		}
		for _, currentTheme := range []fyne.Theme{theme.DefaultTheme(), mytheme.MyTheme{}} {
			app.Settings().SetTheme(currentTheme)
			setting := newAppSetting()
			setting.initialized = true
			setting.sessionRootDir = t.TempDir()
			if err := setting.openConfigurationWindow(); err != nil {
				t.Fatal(err)
			}
			scroll := setting.configurationWindow.Content().(*container.Scroll)
			padded := scroll.Content.(*fyne.Container)
			panels := padded.Objects[0].(*fyne.Container)
			referenceCard := panels.Objects[3].(*widget.Card)
			references := referenceCard.Content.(*fyne.Container)

			findRowObjects := func(object fyne.CanvasObject) (*widget.Entry, *widget.Button) {
				panel := object.(*fyne.Container)
				row := panel.Objects[0].(*fyne.Container)
				var entry *widget.Entry
				var button *widget.Button
				for _, child := range row.Objects {
					if value, ok := child.(*widget.Entry); ok {
						entry = value
					}
					if group, ok := child.(*fyne.Container); ok {
						for _, nested := range group.Objects {
							if value, ok := nested.(*widget.Button); ok {
								button = value
							}
						}
					}
				}
				if entry == nil || button == nil {
					t.Fatal("reference row is missing entry or selection button")
				}
				return entry, button
			}
			resumeEntry, resumeButton := findRowObjects(references.Objects[0])
			knowledgeEntry, knowledgeButton := findRowObjects(references.Objects[2])
			resumeCheck := findCheck(references.Objects[0])
			knowledgeCheck := findCheck(references.Objects[2])
			if resumeCheck == nil || knowledgeCheck == nil {
				t.Fatal("reference checkboxes are missing")
			}
			aligned := func(first, second fyne.CanvasObject) bool {
				firstX := app.Driver().AbsolutePositionForObject(first).X
				secondX := app.Driver().AbsolutePositionForObject(second).X
				return firstX == secondX && first.Size().Width == second.Size().Width
			}
			for _, enabled := range []bool{false, true} {
				resumeCheck.SetChecked(enabled)
				knowledgeCheck.SetChecked(enabled)
				for _, width := range []float32{720, 880, 1100} {
					setting.configurationWindow.Resize(fyne.NewSize(width, 680))
					if !aligned(setting.modelURLEntry, setting.modelAPIKeyEntry) ||
						!aligned(setting.modelURLEntry, setting.modelNameEntry) {
						t.Fatalf("model inputs are not aligned at width %v", width)
					}
					if !aligned(resumeEntry, knowledgeEntry) || !aligned(resumeButton, knowledgeButton) {
						t.Fatalf("reference controls are not aligned at width %v: resume=%v/%v knowledge=%v/%v",
							width, resumeEntry.Position(), resumeEntry.Size(), knowledgeEntry.Position(), knowledgeEntry.Size())
					}
					for _, control := range []fyne.CanvasObject{resumeCheck, knowledgeCheck, resumeButton, knowledgeButton} {
						if control.Size().Width < control.MinSize().Width {
							t.Fatalf("reference control is clipped at width %v", width)
						}
					}
				}
			}
			setting.closeWindows()
		}
	}
}

func TestSystemPromptWrapsWithoutHorizontalScrolling(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	setting := newAppSetting()
	panel := setting.makeModelConfigPanel()
	window := app.NewWindow("设置")
	defer window.Close()
	window.SetContent(panel)
	window.Resize(fyne.NewSize(600, 400))
	window.Show()

	entry := setting.modelPromptEntry
	prompt := strings.Repeat("长提示词和LongUnbrokenText", 40)
	entry.SetText(prompt)
	if entry.Wrapping != fyne.TextWrapBreak || entry.Text != prompt {
		t.Fatal("system prompt must wrap without changing its text")
	}
	var scroll *container.Scroll
	for _, object := range fynetest.WidgetRenderer(entry).Objects() {
		if value, ok := object.(*container.Scroll); ok {
			scroll = value
		}
	}
	if scroll == nil || scroll.Direction != fyne.ScrollVerticalOnly {
		t.Fatal("system prompt must scroll vertically only")
	}
	for _, width := range []float32{420, 780} {
		window.Resize(fyne.NewSize(width, 400))
		if scroll.Direction != fyne.ScrollVerticalOnly || scroll.Content.Size().Width > scroll.Size().Width+1 {
			t.Fatalf("prompt does not fit after resizing to %v: content=%v viewport=%v direction=%v",
				width, scroll.Content.Size(), scroll.Size(), scroll.Direction)
		}
	}
}

func TestConfigurationWindowSizeFitsContentWithinLimit(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	content := canvas.NewRectangle(nil)
	content.SetMinSize(fyne.NewSize(400, 300))
	if got := configurationWindowSize(content); got != fyne.NewSize(720, 300) {
		t.Fatalf("small settings size = %v", got)
	}
	content.SetMinSize(fyne.NewSize(800, 900))
	if got := configurationWindowSize(content); got != fyne.NewSize(800, 680) {
		t.Fatalf("large settings size = %v", got)
	}
}

func TestModelConfigurationIsSavedAfterEditing(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	setting := &appSetting{
		Setting: Setting{
			modelConfigDir: t.TempDir(),
			modelConfig: modelConfig{
				BaseURL: "https://example.com/v1",
				APIKey:  "local-test-key",
				Model:   "initial-model",
			},
		},
	}
	setting.makeModelConfigPanel()
	setting.configurationWindow = app.NewWindow("设置")
	setting.configurationWindow.SetContent(widget.NewLabel("测试设置"))
	setting.configurationWindow.SetOnClosed(func() {
		setting.configurationWindow = nil
	})
	if setting.modelConfigDirty {
		t.Fatal("loading saved values should not mark the configuration as changed")
	}

	setting.modelNameEntry.SetText("updated-model")
	prompt := "第一行提示词\n第二行提示词"
	setting.modelPromptEntry.SetText(prompt)
	if !setting.modelConfigDirty {
		t.Fatal("editing the model name did not mark the configuration as changed")
	}
	setting.closeConfigurationWindow()
	if setting.configurationWindow != nil {
		t.Fatal("settings window did not close after saving")
	}
	if setting.modelConfigDirty {
		t.Fatal("successful save did not clear the changed state")
	}

	saved, err := loadModelConfig(setting.modelConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Model != "updated-model" || saved.APIKey != "local-test-key" || saved.SystemPrompt != prompt {
		t.Fatalf("saved model configuration = %+v", saved)
	}
}
