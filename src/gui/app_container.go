package gui

import (
	"fmt"

	"fyne.io/fyne/v2/container"
)

type appContainer struct {
	initialized map[string]struct{}
	container.DocTabs
}

func newAppContainer() *appContainer {
	at := &appContainer{initialized: make(map[string]struct{})}
	at.CloseIntercept = at.closeHandle
	at.SetTabLocation(container.TabLocationTop)
	at.ExtendBaseWidget(at)
	at.OnSelected = func(item *container.TabItem) {
		item.Content.Refresh()
	}
	return at
}

func (at *appContainer) closeHandle(tab *container.TabItem) {
	for _, app := range registeredApps() {
		if app.GetAppName() == tab.Text && app.OnClose() {
			at.Remove(tab)
			return
		}
	}
}

func (at *appContainer) openDefaultApp() error {
	var firstTab *container.TabItem
	for _, app := range registeredApps() {
		if !app.OpenDefault() {
			continue
		}
		if err := at.initApp(app); err != nil {
			return fmt.Errorf("initialize %s: %w", app.GetAppName(), err)
		}
		tab := app.GetTabItem()
		at.Append(tab)
		if firstTab == nil {
			firstTab = tab
		}
	}

	if firstTab != nil {
		at.Select(firstTab)
	}
	return nil
}

func (at *appContainer) openApp(app appInterface) error {
	for _, item := range at.Items {
		if item.Text == app.GetAppName() {
			at.Select(item)
			return nil
		}
	}

	if err := at.initApp(app); err != nil {
		return err
	}
	tab := app.GetTabItem()
	at.Append(tab)
	at.Select(tab)
	return nil
}

func (at *appContainer) initApp(app appInterface) error {
	name := app.GetAppName()
	if _, ok := at.initialized[name]; ok {
		return nil
	}
	if err := app.LazyInit(); err != nil {
		return err
	}
	at.initialized[name] = struct{}{}
	return nil
}
