package gui

import (
	"fyne.io/fyne/v2"
	fyneDialog "fyne.io/fyne/v2/dialog"
)

const (
	speechMenuName         = "语音"
	realTimeTransMenuName  = "转写"
	interviewAssistantName = "面试"
	recordMenuName         = "录音"
	sessionHistoryMenuName = "历史"
	sessionHistoryItemName = "会话历史"
	aboutMenuName          = "关于"
	githubMenuName         = "github"
)

func newMainMenu() *fyne.MainMenu {
	setting, err := currentAppSetting()
	if err != nil {
		panic(err)
	}
	settingsItem := fyne.NewMenuItem("设置", func() {
		if err := setting.openConfigurationWindow(); err != nil {
			fyneDialog.ShowError(err, currentMainWin.win)
		}
	})
	exitItem := fyne.NewMenuItem("退出", func() {
		currentMainWin.quitHandle()
	})

	realTimeTransItem := fyne.NewMenuItem(realTimeTransMenuName, runRealTimeTrans)
	interviewAssistantItem := fyne.NewMenuItem(interviewAssistantName, runInterviewAssistant)
	recordItem := fyne.NewMenuItem(recordMenuName, runRecorder)
	sessionHistoryItem := fyne.NewMenuItem(sessionHistoryItemName, runSessionHistory)
	versionItem := fyne.NewMenuItem(versionMenuItemName, showVersionInformation)
	githubItem := fyne.NewMenuItem(githubMenuName, openGithubPage)

	return fyne.NewMainMenu(
		fyne.NewMenu(appSettingName, settingsItem, exitItem),
		fyne.NewMenu(speechMenuName, recordItem, realTimeTransItem, interviewAssistantItem),
		fyne.NewMenu(sessionHistoryMenuName, sessionHistoryItem),
		fyne.NewMenu(aboutMenuName, versionItem, githubItem),
	)
}
