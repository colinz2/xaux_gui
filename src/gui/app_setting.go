package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"xaux_gui/pkg/ffaudio"
	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const (
	appSettingName        = "开始"
	sessionRootPreference = "session_root_dir"
)

var _ appInterface = (*appSetting)(nil)

type Setting struct {
	speakerName        string
	microphoneName     string
	speakerSelected    bool
	microphoneSelected bool
	proxyAddr          string
	audioSaveDir       string
	sessionRootDir     string
}

type appSetting struct {
	appAdapter
	Setting
}

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
	if a.audioSaveDir == "" {
		dir := fyne.CurrentApp().Storage().RootURI().Path()
		a.audioSaveDir = filepath.Join(dir, "audio")
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

	speakerRow := a.makeDeviceRow(
		"扬声器", "请选择扬声器设备", devices.PlayBackDevNameList,
		&a.speakerName, &a.speakerSelected,
	)
	microphoneRow := a.makeDeviceRow(
		"麦克风", "请选择麦克风设备", devices.CaptureDevNameList,
		&a.microphoneName, &a.microphoneSelected,
	)
	devicePanel := widget.NewCard(
		"音频设备",
		"选择录音和实时转写使用的设备",
		container.NewVBox(speakerRow, widget.NewSeparator(), microphoneRow),
	)
	sessionPanel := widget.NewCard(
		"会话",
		"每个任务使用独立子目录保存录音、转写和状态",
		a.makeSessionRootRow(),
	)
	a.tabItem = container.NewTabItemWithIcon(
		appSettingName,
		mytheme.ResSettings,
		container.NewPadded(container.NewVBox(devicePanel, sessionPanel)),
	)
	return nil
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

	check := widget.NewCheck("选中", func(checked bool) {
		*selected = checked
	})
	check.SetChecked(*selected)
	return container.NewBorder(
		nil, nil,
		container.NewCenter(widget.NewLabel(label)),
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

func (a *appSetting) OpenDefault() bool {
	return true
}
