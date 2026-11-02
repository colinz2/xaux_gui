package gui

import (
	"fmt"

	"xaux_gui/pkg/ffaudio"
	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

const (
	appID           = "xaux_gui"
	mainWindowTitle = "xaux GUI"
)

var currentMainWin *MainWin

type MainWin struct {
	app fyne.App
	win fyne.Window
	ac  *appContainer
}

func NewMainWin() *MainWin {
	mw := &MainWin{}
	mw.app = app.NewWithID(appID)
	mw.app.SetIcon(mytheme.ResAppIcon)
	mw.app.Settings().SetTheme(mytheme.MyTheme{})

	mw.win = mw.app.NewWindow(mainWindowTitle)
	mw.win.SetMainMenu(newMainMenu())
	mw.ac = newAppContainer()
	mw.win.SetContent(mw.ac)
	mw.win.Resize(fyne.NewSize(1000, 600))
	mw.win.SetPadded(false)
	mw.win.CenterOnScreen()
	mw.win.SetMaster()
	mw.win.SetFixedSize(false)
	mw.win.SetCloseIntercept(mw.quitHandle)
	currentMainWin = mw
	return mw
}

func (m *MainWin) quitHandle() {
	stopActiveTranscription()
	stopActiveRecorder()
	ffaudio.UnInit()
	m.win.Close()
}

func (m *MainWin) Run() {
	if err := ffaudio.Init(); err != nil {
		panic(fmt.Errorf("initialize audio: %w", err))
	}
	if err := m.ac.openDefaultApp(); err != nil {
		panic(fmt.Errorf("open default app: %w", err))
	}
	m.win.ShowAndRun()
}
