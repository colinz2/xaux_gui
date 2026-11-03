package gui

import (
	"fyne.io/fyne/v2"
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
	settingsItem := fyne.NewMenuItem(tr("menu.settings"), func() {
		if err := setting.openConfigurationWindow(); err != nil {
			showLocalizedError(err, currentMainWin.win)
		}
	})
	exitItem := fyne.NewMenuItem(tr("menu.exit"), func() {
		currentMainWin.quitHandle()
	})
	exitItem.IsQuit = true

	realTimeTransItem := fyne.NewMenuItem(tr("menu.transcription"), runRealTimeTrans)
	interviewAssistantItem := fyne.NewMenuItem(tr("menu.interview"), runInterviewAssistant)
	recordItem := fyne.NewMenuItem(tr("menu.record"), runRecorder)
	sessionHistoryItem := fyne.NewMenuItem(tr("menu.session_history"), runSessionHistory)
	versionItem := fyne.NewMenuItem(tr("menu.version"), showVersionInformation)
	githubItem := fyne.NewMenuItem(githubMenuName, openGithubPage)

	return fyne.NewMainMenu(
		fyne.NewMenu(tr("menu.start"), settingsItem, exitItem),
		fyne.NewMenu(tr("menu.speech"), recordItem, realTimeTransItem, interviewAssistantItem),
		fyne.NewMenu(tr("menu.history"), sessionHistoryItem),
		fyne.NewMenu(tr("menu.about"), versionItem, githubItem),
	)
}
