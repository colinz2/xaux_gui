package gui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

type referencePanelConfig struct {
	label       string
	checkLabel  string
	placeholder string
	description string
	pathKey     string
	enabledKey  string
	directory   bool
}

type referenceControls struct {
	check      *widget.Check
	entry      *widget.Entry
	browse     *widget.Button
	info       *widget.Label
	checkWrap  *fyne.Container
	browseWrap *fyne.Container
}

func (a *appSetting) loadInterviewReferencePreferences() {
	preferences := fyne.CurrentApp().Preferences()
	a.resumePath = preferences.String(resumePathPreference)
	a.resumeEnabled = preferences.Bool(resumeEnabledPreference)
	a.knowledgePath = preferences.String(knowledgePathPreference)
	a.knowledgeEnabled = preferences.Bool(knowledgeEnabledPreference)
}

func (a *appSetting) makeInterviewReferencePanel(parent fyne.Window) *fyne.Container {
	resume := newReferenceControls(parent, a.configurationLocale, &a.resumePath, &a.resumeEnabled, referencePanelConfig{
		label: "settings.resume", checkLabel: "settings.use_resume", placeholder: "settings.resume_placeholder",
		description: "settings.resume_description",
		pathKey:     resumePathPreference, enabledKey: resumeEnabledPreference,
	})
	knowledge := newReferenceControls(parent, a.configurationLocale, &a.knowledgePath, &a.knowledgeEnabled, referencePanelConfig{
		label: "settings.knowledge", checkLabel: "settings.knowledge", placeholder: "settings.knowledge_placeholder",
		description: "settings.knowledge_description",
		pathKey:     knowledgePathPreference, enabledKey: knowledgeEnabledPreference, directory: true,
	})
	checkWidth := fyne.Max(resume.check.MinSize().Width, knowledge.check.MinSize().Width)
	buttonWidth := fyne.Max(resume.browse.MinSize().Width, knowledge.browse.MinSize().Width)
	rowHeight := fyne.Max(
		fyne.Max(resume.check.MinSize().Height, knowledge.check.MinSize().Height),
		fyne.Max(resume.browse.MinSize().Height, knowledge.browse.MinSize().Height),
	)
	rowHeight = fyne.Max(rowHeight, fyne.Max(resume.entry.MinSize().Height, knowledge.entry.MinSize().Height))
	result := container.NewVBox(
		resume.panel(checkWidth, buttonWidth, rowHeight),
		widget.NewSeparator(),
		knowledge.panel(checkWidth, buttonWidth, rowHeight),
	)
	a.configurationLocale.add(func() {
		checkWidth := fyne.Max(resume.check.MinSize().Width, knowledge.check.MinSize().Width)
		buttonWidth := fyne.Max(resume.browse.MinSize().Width, knowledge.browse.MinSize().Width)
		for _, controls := range []*referenceControls{&resume, &knowledge} {
			controls.checkWrap.Layout = layout.NewGridWrapLayout(fyne.NewSize(checkWidth, rowHeight))
			controls.browseWrap.Layout = layout.NewGridWrapLayout(fyne.NewSize(buttonWidth, rowHeight))
		}
		result.Refresh()
	})
	return result
}

func (controls *referenceControls) panel(checkWidth, buttonWidth, rowHeight float32) fyne.CanvasObject {
	controls.checkWrap = container.NewGridWrap(fyne.NewSize(checkWidth, rowHeight), controls.check)
	controls.browseWrap = container.NewGridWrap(fyne.NewSize(buttonWidth, rowHeight), controls.browse)
	return container.NewVBox(container.NewBorder(nil, nil, controls.checkWrap, controls.browseWrap, controls.entry), controls.info)
}

func newReferenceControls(parent fyne.Window, bindings *localeBindings, path *string, enabled *bool, config referencePanelConfig) referenceControls {
	entry := widget.NewEntry()
	entry.SetPlaceHolder(tr(config.placeholder))
	entry.SetText(*path)
	entry.OnChanged = func(value string) {
		*path = strings.TrimSpace(value)
		fyne.CurrentApp().Preferences().SetString(config.pathKey, *path)
	}
	choice := "settings.choose_file"
	if config.directory {
		choice = "settings.choose_folder"
	}
	browse := widget.NewButton(tr(choice), func() {
		openReferencePicker(parent, config, entry.SetText)
	})
	check := widget.NewCheck(tr(config.checkLabel), nil)
	check.SetChecked(*enabled)
	updateEnabled := func(value bool) {
		if value {
			entry.Enable()
			browse.Enable()
		} else {
			entry.Disable()
			browse.Disable()
		}
	}
	check.OnChanged = func(value bool) {
		*enabled = value
		fyne.CurrentApp().Preferences().SetBool(config.enabledKey, value)
		updateEnabled(value)
	}
	updateEnabled(*enabled)
	info := widget.NewLabel(tr(config.description))
	info.Importance = widget.LowImportance
	info.Wrapping = fyne.TextWrapBreak
	bindings.add(func() {
		entry.SetPlaceHolder(tr(config.placeholder))
		browse.SetText(tr(choice))
		check.SetText(tr(config.checkLabel))
		info.SetText(tr(config.description))
	})
	return referenceControls{check: check, entry: entry, browse: browse, info: info}
}

func openReferencePicker(parent fyne.Window, config referencePanelConfig, selected func(string)) {
	pickerWindow := fyne.CurrentApp().NewWindow("")
	bindings := &localeBindings{}
	hint := widget.NewLabel("")
	bindings.add(func() {
		name := tr(config.label)
		pickerWindow.SetTitle(trData("settings.choose_reference", map[string]any{"Name": name}))
		hint.SetText(trData("settings.choose_reference_hint", map[string]any{"Name": name}))
	})
	pickerWindow.SetOnClosed(bindings.stop)
	bindings.start()
	pickerWindow.SetContent(container.NewCenter(hint))
	pickerWindow.Resize(fyne.NewSize(800, 600))
	pickerWindow.CenterOnScreen()

	selectURI := func(uri fyne.URI, err error) {
		defer pickerWindow.Close()
		if err != nil {
			showLocalizedError(err, parent)
		} else if uri != nil {
			if uri.Scheme() != "file" {
				showLocalizedError(fmt.Errorf("%s", trData("settings.local_reference_required", map[string]any{"Name": tr(config.label)})), parent)
				return
			}
			selected(uri.Path())
		}
	}
	var picker *dialog.FileDialog
	if config.directory {
		picker = dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			selectURI(uri, err)
		}, pickerWindow)
	} else {
		picker = dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				selectURI(nil, err)
				return
			}
			uri := reader.URI()
			if err := reader.Close(); err != nil {
				selectURI(nil, fmt.Errorf("%s: %w", trData("settings.close_reference_failed", map[string]any{"Name": tr(config.label)}), err))
				return
			}
			selectURI(uri, nil)
		}, pickerWindow)
		picker.SetFilter(storage.NewExtensionFileFilter([]string{".md", ".markdown"}))
	}
	bindings.add(func() {
		picker.SetTitleText(trData("settings.choose_reference", map[string]any{"Name": tr(config.label)}))
		picker.SetConfirmText(tr("common.open"))
		picker.SetDismissText(tr("common.cancel"))
	})
	pickerWindow.Show()
	picker.Show()
}
