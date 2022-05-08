package gui

import (
	"fyne.io/fyne/v2"
)

const (
	kSettingMenuName         = "设置"
	kSpeechMenuName          = "语音"
	kSpeechMenuRealTimeTrans = "实时转写"
	kAbout                   = "关于"
	kGithub                  = "github"
)

type MainMenu struct {
	*fyne.MainMenu
	settingMenu   *fyne.Menu
	setting       *fyne.MenuItem
	speechMenu    *fyne.Menu
	realTimeTrans *fyne.MenuItem
	aboutMenu     *fyne.Menu
	aboutPage     *fyne.MenuItem
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

	// about
	mm.aboutPage = fyne.NewMenuItem(kGithub, func() {
		openGithubPage()
	})
	mm.aboutMenu = fyne.NewMenu(kAbout, mm.aboutPage)

	// main menu
	mm.MainMenu = fyne.NewMainMenu(
		mm.settingMenu,
		mm.speechMenu,
		mm.aboutMenu,
	)
	return &mm
}
