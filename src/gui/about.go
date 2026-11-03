package gui

import (
	"fmt"
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

type thirdPartyComponent struct {
	modulePath string
	name       string
	purpose    string
}

var mainThirdPartyComponents = []thirdPartyComponent{
	{modulePath: "fyne.io/fyne/v2", name: "Fyne", purpose: "桌面界面"},
	{modulePath: "github.com/realzhangm/xaux", name: "xaux", purpose: "语音识别组件"},
	{modulePath: "github.com/sashabaranov/go-openai", name: "go-openai", purpose: "大模型接口"},
	{modulePath: "github.com/sqweek/dialog", name: "dialog", purpose: "系统对话框"},
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

	versionInformationWindow = currentMainWin.app.NewWindow("版本信息")
	versionInformationWindow.SetContent(container.NewVScroll(container.NewPadded(content)))
	versionInformationWindow.Resize(versionInformationWindowSize(versionText))
	versionInformationWindow.CenterOnScreen()
	versionInformationWindow.SetOnClosed(func() {
		versionInformationWindow = nil
	})
	versionInformationWindow.Show()
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
	versionInformationWindow.Close()
	versionInformationWindow = nil
}

func formatVersionInformation(buildInfo *debug.BuildInfo) string {
	version := "开发版"
	goVersion := "未知"
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
		version += "（" + revision + "）"
	}
	if modified {
		version += "（本地代码有修改）"
	}

	lines := []string{
		"软件版本：" + version,
		"Go 版本：" + goVersion,
		"",
		"主要第三方组件：",
	}
	for _, component := range mainThirdPartyComponents {
		componentVersion := dependencyVersion(buildInfo, component.modulePath)
		lines = append(lines, fmt.Sprintf(
			"%s %s：%s",
			component.name,
			componentVersion,
			component.purpose,
		))
	}
	return strings.Join(lines, "\n")
}

func dependencyVersion(buildInfo *debug.BuildInfo, modulePath string) string {
	if buildInfo == nil {
		return "版本未知"
	}
	for _, dependency := range buildInfo.Deps {
		if dependency.Path != modulePath {
			continue
		}
		if dependency.Replace != nil {
			if dependency.Replace.Version == "" {
				return "本地替换"
			}
			return formatDependencyVersion(dependency.Replace.Version)
		}
		return formatDependencyVersion(dependency.Version)
	}
	return "版本未记录"
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
	return "提交 " + revision
}
