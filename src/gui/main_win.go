package gui

import (
	"fmt"
	"log"
	"sync/atomic"

	"xaux_gui/pkg/ffaudio"
	"xaux_gui/src/mytheme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	fyneDialog "fyne.io/fyne/v2/dialog"
)

const (
	appID           = "xaux_gui"
	mainWindowTitle = "xaux GUI"
)

var currentMainWin *MainWin

type MainWin struct {
	app      fyne.App
	win      fyne.Window
	quitting atomic.Bool
}

func NewMainWin() *MainWin {
	mw := &MainWin{}
	mw.app = app.NewWithID(appID)
	if err := resetInterviewDebugLog(mw.app.Storage().RootURI().Path()); err != nil {
		log.Printf("reset interview debug log at startup: %v", err)
	}
	mw.app.SetIcon(mytheme.ResAppIcon)
	mw.app.Settings().SetTheme(mytheme.MyTheme{})

	mw.win = mw.app.NewWindow(mainWindowTitle)
	mw.win.SetMainMenu(newMainMenu())
	mw.win.SetPadded(false)
	mw.win.CenterOnScreen()
	mw.win.SetMaster()
	mw.win.SetFixedSize(false)
	mw.win.SetCloseIntercept(mw.quitHandle)
	currentMainWin = mw
	return mw
}

func (m *MainWin) quitHandle() {
	setting, err := currentAppSetting()
	if err != nil {
		fyneDialog.ShowError(err, m.win)
		return
	}
	if err := setting.saveModelConfigIfDirty(); err != nil {
		fyneDialog.ShowError(err, m.win)
		return
	}
	if !m.quitting.CompareAndSwap(false, true) {
		return
	}
	transcriptionDone := stopActiveTranscription()
	interviewDone := stopActiveInterview()
	go func() {
		if transcriptionDone != nil {
			<-transcriptionDone
		}
		if interviewDone != nil {
			<-interviewDone
		}
		stopActiveRecorder()
		ffaudio.UnInit()
		fyne.DoAndWait(func() {
			closeInterviewSetup()
			closeVersionInformationWindow()
			closeActiveSessionHistory()
			setting.closeWindows()
			m.win.SetCloseIntercept(nil)
			m.win.Close()
		})
	}()
}

func (m *MainWin) Run() {
	if err := ffaudio.Init(); err != nil {
		panic(fmt.Errorf("initialize audio: %w", err))
	}
	setting, err := currentAppSetting()
	if err != nil {
		panic(err)
	}
	if err := setting.LazyInit(); err != nil {
		panic(fmt.Errorf("initialize audio settings: %w", err))
	}
	homePage := newHomePage(setting.audioDevicePanel)
	m.win.SetContent(homePage)
	m.win.Resize(homeWindowSize(homePage))
	m.win.ShowAndRun()
}

func homeWindowSize(homePage fyne.CanvasObject) fyne.Size {
	size := homePage.MinSize()
	if size.Width < 740 {
		size.Width = 740
	}
	size.Height += 24
	if size.Height < 220 {
		size.Height = 220
	}
	return size
}
