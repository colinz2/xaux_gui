package gui

import (
	"os"
	"path"
	"xaux_gui/pkg/ffaudio"
	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const (
	AppSettingName = "设置"
)

var _ appInterface = (*appSetting)(nil)

type Setting struct {
	speakerName          string
	microPhoneName       string
	isSpeakerSelected    bool
	isMicroPhoneSelected bool
	ffDevs               *ffaudio.DevPlaybackAndCapture
	proxyAddr            string
	audioSavaDir         string
}

type appSetting struct {
	appAdapter
	Setting
}

func MakeAppSetting() *appSetting {
	return &appSetting{
		appAdapter: appAdapter{},
		Setting: Setting{
			speakerName:          "",
			microPhoneName:       "",
			isMicroPhoneSelected: true,
			isSpeakerSelected:    true,
			proxyAddr:            "127.0.0.1:11024",
			audioSavaDir:         "",
		},
	}
}

// LazyInit : UI layout
func (a *appSetting) LazyInit() error {
	if len(a.audioSavaDir) == 0 {
		dir := fyne.CurrentApp().Storage().RootURI().Path()
		a.audioSavaDir = path.Join(dir, "audio")
		os.Mkdir(a.audioSavaDir, os.ModePerm)
	}
	a.tabItem = container.NewTabItemWithIcon(AppSettingName, mytheme.ResSettings, nil)

	ffDevs, err := ffaudio.GetDevPlaybackAndCapture()
	if err != nil {
		appQuit()
	} else {
		if len(a.speakerName) == 0 {
			a.speakerName = ffDevs.PlaybackDefault
		}
		if len(a.microPhoneName) == 0 {
			a.microPhoneName = ffDevs.CaptureDefault
		}
	}

	// Speaker. Border layout lets the device entry and its drop-down use all
	// available width instead of the small minimum width assigned by HBox.
	selectEntry := widget.NewSelectEntry(ffDevs.PlayBackDevNameList)
	selectEntry.PlaceHolder = "请选择扬声器设备"
	selectEntry.SetText(a.speakerName)
	selectEntry.Wrapping = fyne.TextWrapOff

	speakerCheck := widget.NewCheck("选中", func(c bool) {
		a.isSpeakerSelected = c
	})
	speakerCheck.Enable()
	speakerCheck.SetChecked(a.isSpeakerSelected)

	// Microphone
	selectEntry2 := widget.NewSelectEntry(ffDevs.CaptureDevNameList)
	selectEntry2.PlaceHolder = "请选择麦克风设备"
	selectEntry2.SetText(a.microPhoneName)
	selectEntry2.Wrapping = fyne.TextWrapOff

	microPhoneCheck := widget.NewCheck("选中", func(c bool) {
		a.isMicroPhoneSelected = c
	})
	microPhoneCheck.Enable()
	microPhoneCheck.SetChecked(a.isMicroPhoneSelected)

	speakerSetting := container.NewBorder(
		nil, nil,
		container.NewCenter(widget.NewLabel("扬声器")),
		container.NewCenter(speakerCheck),
		selectEntry,
	)
	microPhoneSetting := container.NewBorder(
		nil, nil,
		container.NewCenter(widget.NewLabel("麦克风")),
		container.NewCenter(microPhoneCheck),
		selectEntry2,
	)

	devicePanel := widget.NewCard(
		"音频设备",
		"选择录音和实时转写使用的设备",
		container.NewVBox(
			speakerSetting,
			widget.NewSeparator(),
			microPhoneSetting,
		),
	)
	a.tabItem.Content = container.NewPadded(container.NewVBox(devicePanel))

	a.ffDevs = ffDevs
	return nil
}

func (a appSetting) OnClose() bool {
	return true
}

func (a *appSetting) GetAppName() string {
	return AppSettingName
}

func (a *appSetting) OpenDefault() bool {
	return true
}

func (a *appSetting) ShortCut() fyne.Shortcut {
	return nil
}
