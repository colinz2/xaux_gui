package gui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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
	check  *widget.Check
	entry  *widget.Entry
	browse *widget.Button
	info   *widget.Label
}

func (a *appSetting) loadInterviewReferencePreferences() {
	preferences := fyne.CurrentApp().Preferences()
	a.resumePath = preferences.String(resumePathPreference)
	a.resumeEnabled = preferences.Bool(resumeEnabledPreference)
	a.knowledgePath = preferences.String(knowledgePathPreference)
	a.knowledgeEnabled = preferences.Bool(knowledgeEnabledPreference)
}

func (a *appSetting) makeInterviewReferencePanel(parent fyne.Window) *fyne.Container {
	resume := newReferenceControls(parent, &a.resumePath, &a.resumeEnabled, referencePanelConfig{
		label: "简历", checkLabel: "使用简历", placeholder: "选择 Markdown 简历（.md）",
		description: "简历仅用于面试实时提示，并会发送至模型服务。",
		pathKey:     resumePathPreference, enabledKey: resumeEnabledPreference,
	})
	knowledge := newReferenceControls(parent, &a.knowledgePath, &a.knowledgeEnabled, referencePanelConfig{
		label: "面试知识", checkLabel: "面试知识", placeholder: "选择知识文件夹",
		description: "读取文件夹中的 Markdown／文本文件，仅用于面试实时提示。",
		pathKey:     knowledgePathPreference, enabledKey: knowledgeEnabledPreference, directory: true,
	})
	checkWidth := fyne.Max(resume.check.MinSize().Width, knowledge.check.MinSize().Width)
	buttonWidth := fyne.Max(resume.browse.MinSize().Width, knowledge.browse.MinSize().Width)
	rowHeight := fyne.Max(
		fyne.Max(resume.check.MinSize().Height, knowledge.check.MinSize().Height),
		fyne.Max(resume.browse.MinSize().Height, knowledge.browse.MinSize().Height),
	)
	rowHeight = fyne.Max(rowHeight, fyne.Max(resume.entry.MinSize().Height, knowledge.entry.MinSize().Height))
	return container.NewVBox(
		resume.panel(checkWidth, buttonWidth, rowHeight),
		widget.NewSeparator(),
		knowledge.panel(checkWidth, buttonWidth, rowHeight),
	)
}

func (controls referenceControls) panel(checkWidth, buttonWidth, rowHeight float32) fyne.CanvasObject {
	check := container.NewGridWrap(fyne.NewSize(checkWidth, rowHeight), controls.check)
	browse := container.NewGridWrap(fyne.NewSize(buttonWidth, rowHeight), controls.browse)
	return container.NewVBox(container.NewBorder(nil, nil, check, browse, controls.entry), controls.info)
}

func newReferenceControls(parent fyne.Window, path *string, enabled *bool, config referencePanelConfig) referenceControls {
	entry := widget.NewEntry()
	entry.SetPlaceHolder(config.placeholder)
	entry.SetText(*path)
	entry.OnChanged = func(value string) {
		*path = strings.TrimSpace(value)
		fyne.CurrentApp().Preferences().SetString(config.pathKey, *path)
	}
	choice := "选择文件"
	if config.directory {
		choice = "选择文件夹"
	}
	browse := widget.NewButton(choice, func() {
		openReferencePicker(parent, config, entry.SetText)
	})
	check := widget.NewCheck(config.checkLabel, nil)
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
	info := widget.NewLabel(config.description)
	info.Importance = widget.LowImportance
	info.Wrapping = fyne.TextWrapBreak
	return referenceControls{check: check, entry: entry, browse: browse, info: info}
}

func openReferencePicker(parent fyne.Window, config referencePanelConfig, selected func(string)) {
	pickerWindow := fyne.CurrentApp().NewWindow("选择" + config.label)
	pickerWindow.SetContent(container.NewCenter(widget.NewLabel("请选择" + config.label)))
	pickerWindow.Resize(fyne.NewSize(800, 600))
	pickerWindow.CenterOnScreen()

	selectURI := func(uri fyne.URI, err error) {
		defer pickerWindow.Close()
		if err != nil {
			dialog.ShowError(err, parent)
		} else if uri != nil {
			if uri.Scheme() != "file" {
				dialog.ShowError(fmt.Errorf("请选择本地%s", config.label), parent)
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
				selectURI(nil, fmt.Errorf("关闭%s文件失败：%w", config.label, err))
				return
			}
			selectURI(uri, nil)
		}, pickerWindow)
		picker.SetFilter(storage.NewExtensionFileFilter([]string{".md", ".markdown"}))
	}
	pickerWindow.Show()
	picker.Show()
}
