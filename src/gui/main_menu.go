package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
)

const (
	speechMenuName         = "语音"
	realTimeTransMenuName  = "实时转写"
	recordMenuName         = "录音"
	sessionHistoryMenuName = "会话历史"
	aboutMenuName          = "关于"
	githubMenuName         = "github"
)

func newMainMenu() *fyne.MainMenu {
	setting, err := currentAppSetting()
	if err != nil {
		panic(err)
	}
	settingItem := fyne.NewMenuItem(setting.GetAppName(), func() {
		if err := currentMainWin.ac.openApp(setting); err != nil {
			panic(fmt.Errorf("open settings: %w", err))
		}
	})

	realTimeTransItem := fyne.NewMenuItem(realTimeTransMenuName, runRealTimeTrans)
	recordItem := fyne.NewMenuItem(recordMenuName, runRecorder)
	sessionHistoryItem := fyne.NewMenuItem(sessionHistoryMenuName, runSessionHistory)
	githubItem := fyne.NewMenuItem(githubMenuName, openGithubPage)

	return fyne.NewMainMenu(
		fyne.NewMenu(appSettingName, settingItem),
		fyne.NewMenu(speechMenuName, realTimeTransItem, recordItem, sessionHistoryItem),
		fyne.NewMenu(aboutMenuName, githubItem),
	)
}
