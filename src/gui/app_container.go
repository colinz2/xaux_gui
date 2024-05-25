package gui

import (
	"fmt"

	"fyne.io/fyne/v2/container"
)

type appContainer struct {
	appStatusMap map[string]appStatus
	container.DocTabs
}

type appStatus struct {
	lazyInit bool
}

func newAppContainer() *appContainer {
	at := &appContainer{}
	at.appStatusMap = make(map[string]appStatus)
	//at.appTabs = container.NewDocTabs()
	at.CloseIntercept = at.closeHandle
	at.SetTabLocation(container.TabLocationTop)
	at.ExtendBaseWidget(at)
	at.OnSelected = func(item *container.TabItem) {
		// TODO cannot reappear
		// Avoid docTabs invalidation due to theme switching
		item.Content.Refresh()
	}
	return at
}

func (at *appContainer) closeHandle(tab *container.TabItem) {
	fmt.Println("appContainer closeHandle")
	for _, app := range AppResister() {
		if app.GetAppName() == tab.Text {
			if app.OnClose() {
				at.Remove(tab)
			}
		}
	}
}

func (at *appContainer) openDefaultApp() (string, error) {
	var firstTab *container.TabItem
	for _, app := range AppResister() {
		if app.OpenDefault() {
			err := at.initApp(app)
			if err != nil {
				return app.GetAppName(), err
			}
			tab := app.GetTabItem()
			at.Append(tab)
			if firstTab == nil {
				firstTab = tab
			}
		}
	}

	if firstTab != nil {
		at.Select(firstTab)
	}

	return "", nil
}

func (at *appContainer) openApp(app appInterface) error {
	for _, appItem := range at.Items {
		if appItem.Text == app.GetAppName() {
			at.Select(appItem)
			return nil
		}
	}

	err := at.initApp(app)
	if err != nil {
		return err
	}

	tab := app.GetTabItem()
	at.Append(tab)
	at.Select(tab)

	return nil
}

func (at *appContainer) initApp(app appInterface) error {
	st, ok := at.appStatusMap[app.GetAppName()]
	if !ok || !st.lazyInit {
		err := app.LazyInit()
		if err != nil {
			return err
		}
		at.appStatusMap[app.GetAppName()] = appStatus{lazyInit: true}
	}
	return nil
}
