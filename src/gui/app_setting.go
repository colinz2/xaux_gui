package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fyneDialog "fyne.io/fyne/v2/dialog"
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
	heading := canvas.NewText("音频设备", theme.ForegroundColor())
	heading.TextSize = theme.TextSize() * 1.5
	heading.TextStyle = fyne.TextStyle{Bold: true}
	heading.Alignment = fyne.TextAlignLeading
	speakerRow := a.makeDeviceRow(
		"扬声器", "请选择扬声器设备", speakerOptions,
		&a.speakerName, &a.speakerSelected,
	)
	microphoneRow := a.makeDeviceRow(
		"麦克风", "请选择麦克风设备", microphoneOptions,
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

	a.configurationWindow = fyne.CurrentApp().NewWindow("设置")
	a.configurationWindow.SetPadded(false)
	sessionPanel := widget.NewCard(
		"会话保存",
		"每个任务使用独立子目录保存录音、转写和状态",
		a.makeSessionRootRow(),
	)
	modelPanel := a.makeModelConfigPanel()
	referencePanel := widget.NewCard("面试资料", "", a.makeInterviewReferencePanel(a.configurationWindow))
	panelContent := container.NewVBox(sessionPanel, modelPanel, referencePanel)
	paddedContent := container.NewPadded(panelContent)
	content := container.NewVScroll(paddedContent)

	a.configurationWindow.SetContent(content)
	a.configurationWindow.Resize(configurationWindowSize(paddedContent))
	a.configurationWindow.CenterOnScreen()
	a.configurationWindow.SetCloseIntercept(a.closeConfigurationWindow)
	a.configurationWindow.SetOnClosed(func() {
		a.configurationWindow = nil
	})
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
		fyneDialog.ShowError(err, a.configurationWindow)
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
	a.modelURLEntry = widget.NewEntry()
	a.modelURLEntry.SetPlaceHolder(defaultModelBaseURL)
	a.modelURLEntry.SetText(a.modelConfig.BaseURL)
	a.modelAPIKeyEntry = widget.NewPasswordEntry()
	a.modelAPIKeyEntry.SetPlaceHolder("API Key")
	a.modelAPIKeyEntry.SetText(a.modelConfig.APIKey)
	a.modelNameEntry = widget.NewEntry()
	a.modelNameEntry.SetPlaceHolder("例如：doubao-seed-2.1-pro 或 ark-code-latest")
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
	modelFields := container.New(layout.NewFormLayout(),
		widget.NewLabel("API 地址"), a.modelURLEntry,
		widget.NewLabel("API Key"), a.modelAPIKeyEntry,
		widget.NewLabel("默认模型"), a.modelNameEntry,
	)
	rows := container.NewVBox(
		modelFields,
		container.NewBorder(widget.NewLabel("系统提示词（可选）"), nil, nil, nil, a.modelPromptEntry),
	)
	return widget.NewCard(
		"大模型",
		"关闭设置窗口时自动保存。远程服务请使用 HTTPS；API Key 以明文保存在本机，请勿分享配置文件。",
		rows,
	)
}

func (a *appSetting) markModelConfigDirty(string) {
	a.modelConfigDirty = true
}

func (a *appSetting) makeDeviceRow(
	label, placeholder string,
	options []string,
	deviceName *string,
	selected *bool,
) fyne.CanvasObject {
	entry := widget.NewSelectEntry(options)
	entry.PlaceHolder = placeholder
	entry.Wrapping = fyne.TextWrapOff
	entry.OnChanged = func(name string) {
		*deviceName = name
	}
	entry.SetText(*deviceName)

	check := widget.NewCheck("使用", func(checked bool) {
		*selected = checked
	})
	check.SetChecked(*selected)
	deviceLabel := widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewBorder(
		nil, nil,
		container.NewGridWrap(fyne.NewSize(88, entry.MinSize().Height), deviceLabel),
		container.NewCenter(check),
		entry,
	)
}

func (a *appSetting) makeSessionRootRow() fyne.CanvasObject {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("直接输入会话文件的主保存目录")
	entry.SetText(a.sessionRootDir)
	entry.OnChanged = func(path string) {
		a.sessionRootDir = strings.TrimSpace(path)
		fyne.CurrentApp().Preferences().SetString(sessionRootPreference, a.sessionRootDir)
	}
	return container.NewBorder(
		nil, nil,
		container.NewCenter(widget.NewLabel("主保存目录")),
		nil,
		entry,
	)
}

func (a *appSetting) GetAppName() string {
	return appSettingName
}
