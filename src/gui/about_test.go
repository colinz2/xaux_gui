package gui

import (
	"runtime/debug"
	"strings"
	"testing"

	fynetest "fyne.io/fyne/v2/test"
)

func TestFormatVersionInformationIncludesBuildAndDependencyVersions(t *testing.T) {
	buildInfo := &debug.BuildInfo{
		Main:      debug.Module{Version: "(devel)"},
		GoVersion: "go1.26.0",
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.modified", Value: "true"},
		},
		Deps: []*debug.Module{
			{Path: "fyne.io/fyne/v2", Version: "v2.8.2"},
			{Path: "github.com/realzhangm/xaux", Version: "v0.0.0-20261102134721-d19956fae13d"},
			{Path: "github.com/sashabaranov/go-openai", Version: "v1.43.0"},
			{Path: "github.com/sqweek/dialog", Version: "v0.0.0-20260123140253-64c163d53aac"},
		},
	}

	got := formatVersionInformation(buildInfo)
	for _, expected := range []string{
		"软件版本：开发版（0123456）（本地代码有修改）",
		"Go 版本：go1.26.0",
		"Fyne v2.8.2：桌面界面",
		"xaux 提交 d19956fae13d：语音识别组件",
		"go-openai v1.43.0：大模型接口",
		"dialog 提交 64c163d53aac：系统对话框",
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("version information does not contain %q:\n%s", expected, got)
		}
	}
	size := versionInformationWindowSize(got)
	if size.Width != 520 || size.Height > 180 {
		t.Fatalf("version window size = %v, want a compact 520px-wide window", size)
	}
}

func TestVersionInformationOpensAndClosesAsAWindow(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	mainWindow := app.NewWindow("main")

	previousMainWindow := currentMainWin
	previousVersionWindow := versionInformationWindow
	defer func() {
		currentMainWin = previousMainWindow
		versionInformationWindow = previousVersionWindow
	}()
	currentMainWin = &MainWin{app: app, win: mainWindow}
	versionInformationWindow = nil

	showVersionInformation()
	if versionInformationWindow == nil {
		t.Fatal("version information window was not created")
	}
	closeVersionInformationWindow()
	if versionInformationWindow != nil {
		t.Fatal("version information window was not cleared after closing")
	}
}
