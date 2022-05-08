package gui

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"xaux_gui/pkg/ffaudio"
	"xaux_gui/src/mytheme"
)

const (
	AppID       = "xaux_gui"
	MinWinTitle = "xaux GUI"
)

var (
	gMainWin *MainWin
)

type MainWin struct {
	app fyne.App
	win fyne.Window
	ac  *appContainer
	mm  *MainMenu
}

func appQuit() {
	gMainWin.app.Quit()
}

func NewMainWin() *MainWin {
	// app
	var mw = &MainWin{}
	mw.app = app.NewWithID(AppID)
	mw.app.SetIcon(mytheme.ResAppIcon)
	mw.app.Settings().SetTheme(mytheme.MyTheme{})

	// win
	mw.win = mw.app.NewWindow(MinWinTitle)
	mw.mm = newMainMenu()
	mw.win.SetMainMenu(mw.mm.MainMenu)

	mw.ac = newAppContainer()
	content := container.NewBorder(nil, nil, nil, nil, mw.ac)
	mw.win.SetContent(content)

	// resize
	mw.win.Resize(fyne.NewSize(800, 480))
	mw.win.SetPadded(false)
	mw.win.CenterOnScreen()
	mw.win.SetMaster()
	mw.win.SetFixedSize(true)

	// quit
	mw.win.SetCloseIntercept(mw.quitHandle)
	gMainWin = mw
	return mw
}

func (m *MainWin) quitHandle() {
	ffaudio.UnInit()
	m.win.Close()
	fmt.Println("main win close!")
}

func (m *MainWin) Run() {
	ffaudio.Init()
	appName, err := m.ac.openDefaultApp()
	if err != nil {
		panic("run app deault app " + appName)
	}
	m.win.ShowAndRun()
}
