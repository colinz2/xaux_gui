package gui

import (
	"fmt"
	"log"
	"sync/atomic"

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
	app               fyne.App
	win               fyne.Window
	quitting          atomic.Bool
	unsubscribeLocale func()
}

func NewMainWin() *MainWin {
	mw := &MainWin{}
	mw.app = app.NewWithID(appID)
	restoreUILanguage(mw.app.Preferences())
	if err := resetInterviewDebugLog(mw.app.Storage().RootURI().Path()); err != nil {
		log.Printf("reset interview debug log at startup: %s", diagnosticError(err))
	}
	mw.app.SetIcon(mytheme.ResAppIcon)
	mw.app.Settings().SetTheme(mytheme.MyTheme{})

	mw.win = mw.app.NewWindow(mainWindowTitle)
	mw.bindMainMenu()
	mw.win.SetPadded(false)
	mw.win.CenterOnScreen()
	mw.win.SetMaster()
	mw.win.SetFixedSize(false)
	mw.win.SetCloseIntercept(mw.quitHandle)
	currentMainWin = mw
	return mw
}

func (m *MainWin) bindMainMenu() {
	m.win.SetMainMenu(newMainMenu())
	m.unsubscribeLocale = uiLocale.subscribe(func() {
		m.win.SetMainMenu(newMainMenu())
	})
}

func restoreUILanguage(preferences fyne.Preferences) {
	savedLocale := preferences.StringWithFallback(localePreference, localeChinese)
	if err := uiLocale.setLanguage(savedLocale); err != nil {
		log.Printf("load UI language: %v", err)
		if err := uiLocale.setLanguage(localeChinese); err != nil {
			panic(err)
		}
	}
}

func (m *MainWin) quitHandle() {
	setting, err := currentAppSetting()
	if err != nil {
		showLocalizedError(err, m.win)
		return
	}
	if err := setting.saveModelConfigIfDirty(); err != nil {
		showLocalizedError(err, m.win)
		return
	}
	if !m.quitting.CompareAndSwap(false, true) {
		return
	}
	transcriptionDone := stopActiveTranscription()
	interviewDone := stopActiveInterview()
	subtitleDone := stopActiveSubtitle()
	vadDone := stopActiveVADTest()
	go func() {
		if transcriptionDone != nil {
			<-transcriptionDone
		}
		if interviewDone != nil {
			<-interviewDone
		}
		if subtitleDone != nil {
			<-subtitleDone
		}
		if vadDone != nil {
			<-vadDone
		}
		stopActiveRecorder()
		ffaudio.UnInit()
		fyne.DoAndWait(func() {
			closeInterviewSetup()
			closeVersionInformationWindow()
			closeActiveSessionHistory()
			setting.closeWindows()
			setting.homeLocale.stop()
			if m.unsubscribeLocale != nil {
				m.unsubscribeLocale()
			}
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
	setting.homeLocale.start()
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
