package gui

import (
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

type appInterface interface {
	LazyInit() error
	GetTabItem() *container.TabItem
	GetAppName() string
	OpenDefault() bool
	OnClose() bool
	ShortCut() fyne.Shortcut
}

var (
	appRegistry = []appInterface{
		newAppSetting(),
	}
	appRegistryOnce   sync.Once
	appRegistryByName map[string]appInterface
)

func registeredApps() []appInterface {
	return appRegistry
}

func lookupApp(name string) (appInterface, bool) {
	appRegistryOnce.Do(func() {
		appRegistryByName = make(map[string]appInterface, len(appRegistry))
		for _, app := range appRegistry {
			appRegistryByName[app.GetAppName()] = app
		}
	})
	app, ok := appRegistryByName[name]
	return app, ok
}

func currentAppSetting() (*appSetting, error) {
	app, ok := lookupApp(appSettingName)
	if !ok {
		return nil, fmt.Errorf("app %q is not registered", appSettingName)
	}
	setting, ok := app.(*appSetting)
	if !ok || setting == nil {
		return nil, fmt.Errorf("app %q has an invalid settings instance", appSettingName)
	}
	return setting, nil
}

var _ appInterface = (*appAdapter)(nil)

type appAdapter struct {
	tabItem *container.TabItem
}

func (a appAdapter) ShortCut() fyne.Shortcut {
	return nil
}

func (a appAdapter) LazyInit() error {
	panic("implement LazyInit")
}

func (a appAdapter) GetTabItem() *container.TabItem {
	return a.tabItem
}

func (a appAdapter) GetAppName() string {
	panic("implement GetAppName")
}

func (a appAdapter) OpenDefault() bool {
	return false
}

func (a appAdapter) OnClose() bool {
	return true
}
