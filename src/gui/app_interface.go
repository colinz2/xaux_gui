package gui

import (
	"fmt"
	"sync"
)

type appInterface interface {
	LazyInit() error
	GetAppName() string
}

var (
	appRegistry = []appInterface{
		newAppSetting(),
	}
	appRegistryOnce   sync.Once
	appRegistryByName map[string]appInterface
)

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
