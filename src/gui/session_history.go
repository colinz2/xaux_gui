package gui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

const sessionHistoryWindowTitle = "会话历史"

var sessionHistoryWindows struct {
	sync.Mutex
	active *sessionHistoryWin
}

type sessionHistoryWin struct {
	window      fyne.Window
	setting     *appSetting
	sessions    []SessionInfo
	list        *widget.List
	hint        *widget.Label
	name        *widget.Label
	metadata    *widget.Label
	directory   *widget.Label
	detailError *widget.Label
	recordings  *fyne.Container
	transcript  *widget.Entry
}

func runSessionHistory() {
	sessionHistoryWindows.Lock()
	defer sessionHistoryWindows.Unlock()
	if active := sessionHistoryWindows.active; active != nil {
		active.window.Show()
		active.window.RequestFocus()
		return
	}
	setting, err := currentAppSetting()
	if err != nil {
		dialog.ShowError(err, currentMainWin.win)
		return
	}

	win := &sessionHistoryWin{setting: setting}
	win.window = fyne.CurrentApp().NewWindow(sessionHistoryWindowTitle)
	win.window.Resize(fyne.NewSize(1100, 700))
	win.window.CenterOnScreen()
	win.buildContent()
	win.window.SetOnClosed(func() {
		sessionHistoryWindows.Lock()
		if sessionHistoryWindows.active == win {
			sessionHistoryWindows.active = nil
		}
		sessionHistoryWindows.Unlock()
	})
	sessionHistoryWindows.active = win
	win.refresh()
	win.window.Show()
}

func (w *sessionHistoryWin) buildContent() {
	w.hint = widget.NewLabel("")
	w.hint.Wrapping = fyne.TextWrapBreak
	w.list = widget.NewList(
		func() int { return len(w.sessions) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, object fyne.CanvasObject) {
			info := w.sessions[id]
			object.(*widget.Label).SetText(fmt.Sprintf(
				"%s  |  %s  |  %s",
				info.Name,
				info.CreatedAt.Local().Format("2006-01-02 15:04:05"),
				sessionStatusText(info.Status),
			))
		},
	)
	w.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(w.sessions) {
			w.showSession(w.sessions[id])
		}
	}

	refreshButton := widget.NewButton("刷新", w.refresh)
	left := container.NewBorder(
		container.NewHBox(refreshButton),
		w.hint,
		nil, nil,
		w.list,
	)

	w.name = widget.NewLabel("选择一个会话")
	w.name.TextStyle.Bold = true
	w.name.Wrapping = fyne.TextWrapBreak
	w.metadata = widget.NewLabel("")
	w.metadata.Wrapping = fyne.TextWrapBreak
	w.directory = widget.NewLabel("")
	w.directory.Wrapping = fyne.TextWrapBreak
	w.detailError = widget.NewLabel("")
	w.detailError.Wrapping = fyne.TextWrapBreak
	w.recordings = container.NewVBox()
	w.transcript = widget.NewMultiLineEntry()
	w.transcript.Wrapping = fyne.TextWrapBreak
	w.transcript.SetMinRowsVisible(12)
	w.transcript.SetPlaceHolder("无转写内容")
	w.transcript.Disable()
	details := container.NewVBox(
		w.name,
		w.metadata,
		w.directory,
		w.detailError,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("录音文件", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		w.recordings,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("转写结果", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		w.transcript,
	)

	split := container.NewHSplit(left, container.NewVScroll(details))
	split.Offset = 0.42
	w.window.SetContent(split)
}

func (w *sessionHistoryWin) refresh() {
	sessions, err := LoadSessions(w.setting.sessionRootDir)
	w.list.UnselectAll()
	w.sessions = sessions
	w.clearDetails()
	if err != nil {
		w.hint.SetText("加载失败：" + err.Error())
	} else if len(sessions) == 0 {
		w.hint.SetText("暂无会话")
	} else {
		w.hint.SetText(fmt.Sprintf("共 %d 个会话；主目录：%s", len(sessions), w.setting.sessionRootDir))
	}
	w.list.Refresh()
}

func (w *sessionHistoryWin) clearDetails() {
	w.name.SetText("选择一个会话")
	w.metadata.SetText("")
	w.directory.SetText("")
	w.detailError.SetText("")
	w.recordings.Objects = nil
	w.recordings.Refresh()
	w.transcript.SetText("")
}

func (w *sessionHistoryWin) showSession(info SessionInfo) {
	w.name.SetText(info.Name)
	endedAt := info.UpdatedAt
	if info.Status == SessionStatusRunning {
		endedAt = time.Now()
	}
	duration := formatSessionDuration(endedAt.Sub(info.CreatedAt))
	w.metadata.SetText(fmt.Sprintf(
		"创建时间：%s\n持续时长：%s\n任务类型：%s    状态：%s",
		info.CreatedAt.Local().Format("2006-01-02 15:04:05"),
		duration,
		sessionKindText(info.Kind),
		sessionStatusText(info.Status),
	))
	w.directory.SetText("保存目录：" + info.Directory)
	w.detailError.SetText("")
	if info.Error != "" {
		w.detailError.SetText("任务错误：" + info.Error)
	}

	rows := make([]fyne.CanvasObject, 0, len(info.Recordings))
	for _, recording := range info.Recordings {
		fileName := filepath.Base(recording)
		path := filepath.Join(info.Directory, fileName)
		label := widget.NewLabel(fileName)
		label.Wrapping = fyne.TextWrapBreak
		play := widget.NewButton("播放", func() {
			if _, err := os.Stat(path); err != nil {
				dialog.ShowError(err, w.window)
				return
			}
			uri, err := url.Parse(storage.NewFileURI(path).String())
			if err == nil {
				err = fyne.CurrentApp().OpenURL(uri)
			}
			if err != nil {
				dialog.ShowError(err, w.window)
			}
		})
		rows = append(rows, container.NewBorder(nil, nil, label, play))
	}
	if len(rows) == 0 {
		rows = append(rows, widget.NewLabel("无录音文件"))
	}
	w.recordings.Objects = rows
	w.recordings.Refresh()

	w.transcript.SetText("")
	if info.TranscriptFile == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(info.Directory, filepath.Base(info.TranscriptFile)))
	if err != nil {
		w.detailError.SetText("读取转写失败：" + err.Error())
		return
	}
	w.transcript.SetText(string(data))
}
