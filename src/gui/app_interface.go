package gui

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

type appInterface interface {
	LazyInit() error // run when click on menu or toolbar to run the app
	GetTabItem() *container.TabItem
	GetAppName() string
	OpenDefault() bool
	OnClose() bool
	ShortCut() fyne.Shortcut
}

var (
	_appRegister = []appInterface{
		MakeAppSetting(),
	}
	_appRegisterMap map[string]appInterface = nil
	_mapMutex                               = sync.Mutex{}
)

func AppResister() []appInterface {
	return _appRegister
}

func AppResisterMap() map[string]appInterface {
	var m map[string]appInterface

	_mapMutex.Lock()
	if _appRegisterMap != nil {
		m = _appRegisterMap
		_mapMutex.Unlock()
		return m
	}
	_appRegisterMap = make(map[string]appInterface)
	for i := range _appRegister {
		_appRegisterMap[_appRegister[i].GetAppName()] = _appRegister[i]
	}
	m = _appRegisterMap
	_mapMutex.Unlock()
	return m
}

var _ appInterface = (*appAdapter)(nil)

type appAdapter struct {
	tabItem *container.TabItem
}

func (a appAdapter) ShortCut() fyne.Shortcut {
	panic("implement ShortCut")
}

func (a appAdapter) LazyInit() error {
	panic("implement LazyInit")
}

func (a appAdapter) GetTabItem() *container.TabItem {
	return a.tabItem
}

func (a appAdapter) GetAppName() string {
	panic("implement GetAppName()")
}

func (a appAdapter) OpenDefault() bool {
	return false
}

func (a appAdapter) OnClose() bool {
	return true
}
