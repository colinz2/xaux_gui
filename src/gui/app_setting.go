package gui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"xaux_gui/pkg/ffaudio"
)

const (
	appSettingName             = "开始"
	sessionRootPreference      = "session_root_dir"
	resumePathPreference       = "interview_resume_path"
	resumeEnabledPreference    = "interview_resume_enabled"
	knowledgePathPreference    = "interview_knowledge_path"
	knowledgeEnabledPreference = "interview_knowledge_enabled"
)

type Setting struct {
	speakerName        string
	microphoneName     string
	speakerSelected    bool
	microphoneSelected bool
	proxyAddr          string
	audioSaveDir       string
	sessionRootDir     string
	modelConfigDir     string
	modelConfig        modelConfig
	audioDevicePanel   fyne.CanvasObject
	modelURLEntry      *widget.Entry
	modelAPIKeyEntry   *widget.Entry
	modelNameEntry     *widget.Entry
	modelPromptEntry   *widget.Entry
}

type appSetting struct {
	Setting
	initialized         bool
	configurationWindow fyne.Window
	modelConfigDirty    bool
	resumePath          string
	resumeEnabled       bool
	knowledgePath       string
	knowledgeEnabled    bool
	homeLocale          *localeBindings
	configurationLocale *localeBindings
}

var _ appInterface = (*appSetting)(nil)

func newAppSetting() *appSetting {
	return &appSetting{
		Setting: Setting{
			microphoneSelected: true,
			speakerSelected:    true,
			proxyAddr:          "127.0.0.1:11024",
		},
	}
}

func (a *appSetting) LazyInit() error {
	if a.initialized {
		return nil
	}

	storageRootDir := fyne.CurrentApp().Storage().RootURI().Path()
	a.loadInterviewReferencePreferences()
	if a.audioSaveDir == "" {
		a.audioSaveDir = filepath.Join(storageRootDir, "audio")
		if err := os.MkdirAll(a.audioSaveDir, 0o755); err != nil {
			return fmt.Errorf("create audio directory: %w", err)
		}
	}

	if a.sessionRootDir == "" {
		a.sessionRootDir = fyne.CurrentApp().Preferences().StringWithFallback(
			sessionRootPreference,
			filepath.Join(a.audioSaveDir, "sessions"),
		)
	}
	if err := os.MkdirAll(a.sessionRootDir, 0o755); err != nil {
		return fmt.Errorf("create session root directory: %w", err)
	}

	devices, err := ffaudio.GetDevPlaybackAndCapture()
	if err != nil {
		return fmt.Errorf("enumerate audio devices: %w", err)
	}
	if a.speakerName == "" {
		a.speakerName = devices.PlaybackDefault
	}
	if a.microphoneName == "" {
		a.microphoneName = devices.CaptureDefault
	}

	a.audioDevicePanel = a.makeAudioDevicePanel(
		devices.PlayBackDevNameList,
		devices.CaptureDevNameList,
	)
	a.initialized = true
	return nil
}

func (a *appSetting) makeAudioDevicePanel(
	speakerOptions, microphoneOptions []string,
) fyne.CanvasObject {
	bindings := &localeBindings{}
	a.homeLocale = bindings
	heading := canvas.NewText("", theme.ForegroundColor())
	bindings.add(func() {
		heading.Text = tr("home.audio_devices")
		heading.Refresh()
	})
	heading.TextSize = theme.TextSize() * 1.5
	heading.TextStyle = fyne.TextStyle{Bold: true}
	heading.Alignment = fyne.TextAlignLeading
	speakerRow := a.makeDeviceRow(
		bindings, "home.speaker", "home.speaker_placeholder", speakerOptions,
		&a.speakerName, &a.speakerSelected,
	)
	microphoneRow := a.makeDeviceRow(
		bindings, "home.microphone", "home.microphone_placeholder", microphoneOptions,
		&a.microphoneName, &a.microphoneSelected,
	)
	return container.NewVBox(
		heading,
		widget.NewSeparator(),
		speakerRow,
		widget.NewSeparator(),
		microphoneRow,
	)
}

func (a *appSetting) openConfigurationWindow() error {
	if err := a.LazyInit(); err != nil {
		return err
	}
	if a.configurationWindow != nil {
		a.configurationWindow.Show()
		a.configurationWindow.RequestFocus()
		return nil
	}
	if err := a.reloadModelConfig(); err != nil {
		return fmt.Errorf("load model configuration: %w", err)
	}

	a.configurationLocale = &localeBindings{}
	bindings := a.configurationLocale
	a.configurationWindow = fyne.CurrentApp().NewWindow(tr("settings.title"))
	a.configurationWindow.SetPadded(false)
	sessionPanel := widget.NewCard(
		"",
		"",
		a.makeSessionRootRow(),
	)
	bindings.add(func() {
		sessionPanel.SetTitle(tr("settings.session"))
		sessionPanel.SetSubTitle(tr("settings.session_description"))
	})
	modelPanel := a.makeModelConfigPanel()
	referencePanel := widget.NewCard("", "", a.makeInterviewReferencePanel(a.configurationWindow))
	bindings.add(func() { referencePanel.SetTitle(tr("settings.interview_references")) })
	languageLabel := widget.NewLabel("")
	bindings.add(func() { languageLabel.SetText(tr("language.setting")) })
	languageSelect := widget.NewSelect([]string{"简体中文", "English"}, nil)
	if uiLocale.language() == localeEnglish {
		languageSelect.SetSelected("English")
	} else {
		languageSelect.SetSelected("简体中文")
	}
	languageSelect.OnChanged = func(value string) {
		var locale string
		switch value {
		case "简体中文":
			locale = localeChinese
		case "English":
			locale = localeEnglish
		default:
			log.Printf("unsupported UI language selection %q", value)
			return
		}
		if err := uiLocale.setLanguage(locale); err != nil {
			showLocalizedError(err, a.configurationWindow)
			return
		}
		fyne.CurrentApp().Preferences().SetString(localePreference, locale)
	}
	languageRow := container.NewBorder(nil, nil, languageLabel, nil, languageSelect)
	panelContent := container.NewVBox(languageRow, sessionPanel, modelPanel, referencePanel)
	paddedContent := container.NewPadded(panelContent)
	content := container.NewVScroll(paddedContent)

	a.configurationWindow.SetContent(content)
	a.configurationWindow.Resize(configurationWindowSize(paddedContent))
	a.configurationWindow.CenterOnScreen()
	a.configurationWindow.SetCloseIntercept(a.closeConfigurationWindow)
	a.configurationWindow.SetOnClosed(func() {
		bindings.stop()
		a.configurationWindow = nil
		a.configurationLocale = nil
	})
	bindings.add(func() {
		a.configurationWindow.SetTitle(tr("settings.title"))
		paddedContent.Refresh()
	})
	bindings.start()
	a.configurationWindow.Show()
	return nil
}

func configurationWindowSize(content fyne.CanvasObject) fyne.Size {
	size := content.MinSize()
	if size.Width < 720 {
		size.Width = 720
	}
	if size.Height > 680 {
		size.Height = 680
	}
	return size
}

func (a *appSetting) closeConfigurationWindow() {
	if err := a.saveModelConfigIfDirty(); err != nil {
		showLocalizedError(err, a.configurationWindow)
		return
	}
	window := a.configurationWindow
	window.SetCloseIntercept(nil)
	window.Close()
}

func (a *appSetting) saveModelConfigIfDirty() error {
	if !a.modelConfigDirty {
		return nil
	}
	config := modelConfig{
		BaseURL:      strings.TrimSpace(a.modelURLEntry.Text),
		APIKey:       strings.TrimSpace(a.modelAPIKeyEntry.Text),
		Model:        strings.TrimSpace(a.modelNameEntry.Text),
		SystemPrompt: strings.TrimSpace(a.modelPromptEntry.Text),
	}
	if err := saveModelConfig(a.modelConfigDir, config); err != nil {
		return fmt.Errorf("save model configuration: %w", err)
	}
	a.modelConfig = config
	a.modelConfigDirty = false
	return nil
}

func (a *appSetting) closeWindows() {
	if a.configurationWindow != nil {
		a.configurationLocale.stop()
		a.configurationWindow.SetOnClosed(nil)
		a.configurationWindow.Close()
		a.configurationWindow = nil
	}
}

func (a *appSetting) reloadModelConfig() error {
	a.modelConfigDir = fyne.CurrentApp().Storage().RootURI().Path()
	config, err := loadModelConfigWithMigration(a.modelConfigDir, a.sessionRootDir)
	if err != nil {
		return err
	}
	a.modelConfig = config
	a.modelConfigDirty = false
	return nil
}

func (a *appSetting) makeModelConfigPanel() fyne.CanvasObject {
	bindings := a.configurationLocale
	a.modelURLEntry = widget.NewEntry()
	a.modelURLEntry.SetPlaceHolder(defaultModelBaseURL)
	a.modelURLEntry.SetText(a.modelConfig.BaseURL)
	a.modelAPIKeyEntry = widget.NewPasswordEntry()
	a.modelAPIKeyEntry.SetPlaceHolder("API Key")
	a.modelAPIKeyEntry.SetText(a.modelConfig.APIKey)
	a.modelNameEntry = widget.NewEntry()
	bindings.add(func() { a.modelNameEntry.SetPlaceHolder(tr("settings.model_name_placeholder")) })
	a.modelNameEntry.SetText(a.modelConfig.Model)
	a.modelURLEntry.OnChanged = a.markModelConfigDirty
	a.modelAPIKeyEntry.OnChanged = a.markModelConfigDirty
	a.modelNameEntry.OnChanged = a.markModelConfigDirty
	a.modelPromptEntry = widget.NewMultiLineEntry()
	a.modelPromptEntry.SetMinRowsVisible(4)
	a.modelPromptEntry.Wrapping = fyne.TextWrapBreak
	a.modelPromptEntry.SetPlaceHolder(defaultInterviewSystemPrompt)
	a.modelPromptEntry.SetText(a.modelConfig.SystemPrompt)
	a.modelPromptEntry.OnChanged = a.markModelConfigDirty
	apiLabel := widget.NewLabel("")
	modelLabel := widget.NewLabel("")
	promptLabel := widget.NewLabel("")
	bindings.add(func() {
		apiLabel.SetText(tr("settings.api_url"))
		modelLabel.SetText(tr("settings.model_name"))
		promptLabel.SetText(tr("settings.system_prompt"))
	})
	modelFields := container.New(layout.NewFormLayout(),
		apiLabel, a.modelURLEntry,
		widget.NewLabel("API Key"), a.modelAPIKeyEntry,
		modelLabel, a.modelNameEntry,
	)
	rows := container.NewVBox(
		modelFields,
		container.NewBorder(promptLabel, nil, nil, nil, a.modelPromptEntry),
	)
	card := widget.NewCard("", "", rows)
	bindings.add(func() {
		card.SetTitle(tr("settings.model"))
		card.SetSubTitle(tr("settings.model_description"))
	})
	return card
}

func (a *appSetting) markModelConfigDirty(string) {
	a.modelConfigDirty = true
}

func (a *appSetting) makeDeviceRow(
	bindings *localeBindings, label, placeholder string,
	options []string,
	deviceName *string,
	selected *bool,
) fyne.CanvasObject {
	entry := widget.NewSelectEntry(options)
	entry.PlaceHolder = tr(placeholder)
	entry.Wrapping = fyne.TextWrapOff
	entry.OnChanged = func(name string) {
		*deviceName = name
	}
	entry.SetText(*deviceName)

	check := widget.NewCheck(tr("home.use_device"), func(checked bool) {
		*selected = checked
	})
	check.SetChecked(*selected)
	deviceLabel := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	labelWrap := container.NewGridWrap(fyne.NewSize(88, entry.MinSize().Height), deviceLabel)
	bindings.add(func() {
		deviceLabel.SetText(tr(label))
		speakerWidth := widget.NewLabelWithStyle(tr("home.speaker"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}).MinSize().Width
		microphoneWidth := widget.NewLabelWithStyle(tr("home.microphone"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}).MinSize().Width
		labelWrap.Layout = layout.NewGridWrapLayout(fyne.NewSize(fyne.Max(88, fyne.Max(speakerWidth, microphoneWidth)), entry.MinSize().Height))
		labelWrap.Refresh()
		entry.PlaceHolder = tr(placeholder)
		entry.Refresh()
		check.SetText(tr("home.use_device"))
	})
	return container.NewBorder(
		nil, nil,
		labelWrap,
		container.NewCenter(check),
		entry,
	)
}

func (a *appSetting) makeSessionRootRow() fyne.CanvasObject {
	entry := widget.NewEntry()
	label := widget.NewLabel("")
	a.configurationLocale.add(func() {
		entry.SetPlaceHolder(tr("settings.session_root_placeholder"))
		label.SetText(tr("settings.session_root"))
	})
	entry.SetText(a.sessionRootDir)
	entry.OnChanged = func(path string) {
		a.sessionRootDir = strings.TrimSpace(path)
		fyne.CurrentApp().Preferences().SetString(sessionRootPreference, a.sessionRootDir)
	}
	return container.NewBorder(
		nil, nil,
		container.NewCenter(label),
		nil,
		entry,
	)
}

func (a *appSetting) GetAppName() string {
	return appSettingName
}
