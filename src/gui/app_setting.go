package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/realzhangm/xaux/pkg/ffaudio"
	"os"
	"path"
	"xaux_gui/src/mytheme"
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
			a.speakerName = ffDevs.PlayBackDefault
		}
		if len(a.microPhoneName) == 0 {
			a.microPhoneName = ffDevs.CaptureDefault
		}
	}

	/// speaker
	selectEntry := widget.NewSelectEntry(ffDevs.PlayBackDevNameList)
	selectEntry.PlaceHolder = a.speakerName
	selectEntry.Wrapping = fyne.TextWrapOff

	speakerCheck := widget.NewCheck("选中", func(c bool) {
		a.isSpeakerSelected = c
	})
	speakerCheck.Enable()
	speakerCheck.SetChecked(a.isSpeakerSelected)

	/// microPhone
	selectEntry2 := widget.NewSelectEntry(ffDevs.CaptureDevNameList)
	selectEntry2.PlaceHolder = a.microPhoneName
	selectEntry2.Wrapping = fyne.TextWrapOff

	microPhoneCheck := widget.NewCheck("选中", func(c bool) {
		a.isMicroPhoneSelected = c
	})
	microPhoneCheck.Enable()
	microPhoneCheck.SetChecked(a.isMicroPhoneSelected)

	speakerSetting := container.NewHBox(
		widget.NewLabel("扬声器"),
		selectEntry,
		speakerCheck,
		layout.NewSpacer(),
		widget.NewLabel("麦克风"),
		selectEntry2,
		microPhoneCheck,
	)

	a.tabItem.Content = container.NewGridWithRows(
		1,
		container.NewVBox(
			speakerSetting,
		))

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
