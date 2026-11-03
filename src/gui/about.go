package gui

import (
	"runtime/debug"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"xaux_gui/pkg/utils"
)

const versionMenuItemName = "版本显示"

var versionInformationWindow fyne.Window
var versionInformationLocale *localeBindings

type thirdPartyComponent struct {
	modulePath string
	name       string
	purpose    string
}

var mainThirdPartyComponents = []thirdPartyComponent{
	{modulePath: "fyne.io/fyne/v2", name: "Fyne", purpose: "about.desktop"},
	{modulePath: "github.com/realzhangm/xaux", name: "xaux", purpose: "about.asr"},
	{modulePath: "github.com/sashabaranov/go-openai", name: "go-openai", purpose: "about.model"},
	{modulePath: "github.com/sqweek/dialog", name: "dialog", purpose: "about.dialog"},
}

func openGithubPage() {
	utils.OpenURL("https://github.com/realzhangm/xaux_gui")
}

func showVersionInformation() {
	if versionInformationWindow != nil {
		versionInformationWindow.Show()
		versionInformationWindow.RequestFocus()
		return
	}

	buildInfo, _ := debug.ReadBuildInfo()
	versionText := formatVersionInformation(buildInfo)
	content := widget.NewLabel(versionText)
	content.Wrapping = fyne.TextWrapBreak
	content.Selectable = true

	versionInformationWindow = currentMainWin.app.NewWindow(tr("about.title"))
	versionInformationLocale = &localeBindings{}
	versionInformationLocale.add(func() {
		versionInformationWindow.SetTitle(tr("about.title"))
		content.SetText(formatVersionInformation(buildInfo))
	})
	versionInformationWindow.SetContent(container.NewVScroll(container.NewPadded(content)))
	versionInformationWindow.Resize(versionInformationWindowSize(versionText))
	versionInformationWindow.CenterOnScreen()
	versionInformationWindow.SetOnClosed(func() {
		versionInformationLocale.stop()
		versionInformationLocale = nil
		versionInformationWindow = nil
	})
	versionInformationWindow.Show()
	versionInformationLocale.start()
}

func versionInformationWindowSize(content string) fyne.Size {
	lineCount := strings.Count(content, "\n") + 1
	lineHeight := theme.TextSize() + theme.Padding()
	height := float32(lineCount)*lineHeight + theme.Padding()*2
	return fyne.NewSize(520, height)
}

func closeVersionInformationWindow() {
	if versionInformationWindow == nil {
		return
	}
	versionInformationWindow.SetOnClosed(nil)
	versionInformationLocale.stop()
	versionInformationLocale = nil
	versionInformationWindow.Close()
	versionInformationWindow = nil
}

func formatVersionInformation(buildInfo *debug.BuildInfo) string {
	version := tr("about.development")
	goVersion := tr("about.unknown")
	revision := ""
	modified := false
	if buildInfo != nil {
		if buildInfo.Main.Version != "" && buildInfo.Main.Version != "(devel)" {
			version = buildInfo.Main.Version
		}
		goVersion = buildInfo.GoVersion
		for _, setting := range buildInfo.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
	}
	if revision != "" {
		if len(revision) > 7 {
			revision = revision[:7]
		}
		version += trData("about.revision_suffix", map[string]any{"Revision": revision})
	}
	if modified {
		version += tr("about.modified")
	}

	lines := []string{
		trData("about.version", map[string]any{"Version": version}),
		trData("about.go_version", map[string]any{"Version": goVersion}),
		"",
		tr("about.components"),
	}
	for _, component := range mainThirdPartyComponents {
		componentVersion := dependencyVersion(buildInfo, component.modulePath)
		lines = append(lines, trData("about.component_line", map[string]any{
			"Name": component.name, "Version": componentVersion, "Purpose": tr(component.purpose),
		}))
	}
	return strings.Join(lines, "\n")
}

func dependencyVersion(buildInfo *debug.BuildInfo, modulePath string) string {
	if buildInfo == nil {
		return tr("about.version_unknown")
	}
	for _, dependency := range buildInfo.Deps {
		if dependency.Path != modulePath {
			continue
		}
		if dependency.Replace != nil {
			if dependency.Replace.Version == "" {
				return tr("about.local_replacement")
			}
			return formatDependencyVersion(dependency.Replace.Version)
		}
		return formatDependencyVersion(dependency.Version)
	}
	return tr("about.version_missing")
}

func formatDependencyVersion(version string) string {
	if !strings.HasPrefix(version, "v0.0.0-") {
		return version
	}
	separator := strings.LastIndex(version, "-")
	if separator < 0 {
		return version
	}
	revision := version[separator+1:]
	if len(revision) > 12 {
		revision = revision[:12]
	}
	return trData("about.revision", map[string]any{"Revision": revision})
}
