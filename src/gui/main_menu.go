package gui

import (
	"fyne.io/fyne/v2"
)

const (
	kSettingMenuName         = "设置"
	kSpeechMenuName          = "语音"
	kSpeechMenuRealTimeTrans = "实时转写"
)

type MainMenu struct {
	*fyne.MainMenu
	settingMenu   *fyne.Menu
	setting       *fyne.MenuItem
	speechMenu    *fyne.Menu
	realTimeTrans *fyne.MenuItem
}

func newMainMenu() *MainMenu {
	var mm MainMenu

	appMap := AppResisterMap()
	appSetting, exist := appMap[AppSettingName]
	if !exist {
		panic("")
	}
	// setting
	mm.setting = fyne.NewMenuItem(appSetting.GetAppName(), func() {
		err := gMainWin.ac.openApp(appSetting)
		if err != nil {
			panic(err)
		}
	})
	mm.settingMenu = fyne.NewMenu(kSettingMenuName, mm.setting)

	// speech
	mm.realTimeTrans = fyne.NewMenuItem(kSpeechMenuRealTimeTrans, func() {
		runRealTimeTrans()
	})
	mm.speechMenu = fyne.NewMenu(kSpeechMenuName, mm.realTimeTrans)

	// main menu
	mm.MainMenu = fyne.NewMainMenu(
		mm.settingMenu,
		mm.speechMenu,
	)
	return &mm
}
