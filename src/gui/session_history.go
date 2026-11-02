package gui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const sessionHistoryWindowTitle = "会话历史"
const wavHeaderSize = 44

var sessionHistoryWindows struct {
	sync.Mutex
	active *sessionHistoryWin
}

type sessionHistoryWin struct {
	window       fyne.Window
	setting      *appSetting
	sessions     []SessionInfo
	list         *widget.List
	searchEntry  *widget.Entry
	hint         *widget.Label
	name         *widget.Label
	metadata     *widget.Label
	directory    *widget.Label
	detailError  *widget.Label
	recordings   *fyne.Container
	transcript   *widget.Entry
	details      *fyne.Container
	deleteButton *widget.Button
	selected     *SessionInfo
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
	win.searchSessions()
	win.window.Show()
}

func (w *sessionHistoryWin) buildContent() {
	w.hint = widget.NewLabel("")
	w.hint.Wrapping = fyne.TextWrapBreak
	w.list = widget.NewList(
		func() int { return len(w.sessions) },
		func() fyne.CanvasObject { return sessionHistoryListItem(SessionInfo{}) },
		func(id widget.ListItemID, object fyne.CanvasObject) {
			updateSessionHistoryListItem(object.(*fyne.Container), w.sessions[id])
		},
	)
	w.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(w.sessions) {
			w.showSession(w.sessions[id])
		}
	}

	w.searchEntry = widget.NewEntry()
	w.searchEntry.SetPlaceHolder("搜索会话名称或转写内容")
	w.searchEntry.OnChanged = func(string) {
		w.searchSessions()
	}
	w.searchEntry.OnSubmitted = func(string) {
		w.searchSessions()
	}
	searchButton := widget.NewButtonWithIcon("搜索", theme.SearchIcon(), w.searchSessions)
	left := container.NewBorder(
		container.NewBorder(nil, nil, nil, searchButton, w.searchEntry),
		w.hint,
		nil, nil,
		w.list,
	)

	w.name = widget.NewLabel("选择一个会话")
	w.name.TextStyle.Bold = true
	w.name.Wrapping = fyne.TextWrapBreak
	w.deleteButton = widget.NewButtonWithIcon("删除会话", theme.DeleteIcon(), w.confirmDeleteSession)
	w.deleteButton.Disable()
	titleRow := container.NewBorder(nil, nil, nil, w.deleteButton, w.name)
	w.metadata = widget.NewLabel("")
	w.metadata.Wrapping = fyne.TextWrapBreak
	w.directory = widget.NewLabel("")
	w.directory.Wrapping = fyne.TextWrapBreak
	w.directory.Selectable = true
	w.detailError = widget.NewLabel("")
	w.detailError.Wrapping = fyne.TextWrapBreak
	w.recordings = container.NewVBox()
	w.transcript = widget.NewMultiLineEntry()
	w.transcript.Wrapping = fyne.TextWrapBreak
	w.transcript.SetMinRowsVisible(12)
	w.transcript.SetPlaceHolder("无转写内容")
	w.transcript.Disable()
	recordingsHeader := widget.NewLabelWithStyle("录音文件", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	transcriptHeader := widget.NewLabelWithStyle("转写结果", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	transcriptPanel := container.NewBorder(transcriptHeader, nil, nil, nil, w.transcript)
	detailsHeader := container.NewVBox(
		titleRow,
		w.metadata,
		w.directory,
		w.detailError,
		widget.NewSeparator(),
		recordingsHeader,
		w.recordings,
		widget.NewSeparator(),
	)

	w.details = container.NewBorder(detailsHeader, nil, nil, nil, transcriptPanel)
	split := container.NewHSplit(left, w.details)
	split.Offset = 0.42
	w.window.SetContent(split)
}

func sessionHistoryListItem(info SessionInfo) *fyne.Container {
	name := widget.NewLabel("")
	name.Wrapping = fyne.TextWrapOff
	name.Truncation = fyne.TextTruncateEllipsis
	date := widget.NewLabel("")
	date.Alignment = fyne.TextAlignTrailing
	status := widget.NewLabel("")
	status.Alignment = fyne.TextAlignTrailing
	metadata := container.NewHBox(date, status)
	row := container.NewBorder(nil, nil, nil, metadata, name)
	updateSessionHistoryListItem(row, info)
	return row
}

func updateSessionHistoryListItem(row *fyne.Container, info SessionInfo) {
	name := row.Objects[0].(*widget.Label)
	metadata := row.Objects[1].(*fyne.Container)
	date := metadata.Objects[0].(*widget.Label)
	status := metadata.Objects[1].(*widget.Label)

	name.SetText(info.Name)
	date.SetText(info.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	status.SetText(sessionStatusText(info.Status))
}

func (w *sessionHistoryWin) searchSessions() {
	sessions, err := LoadSessions(w.setting.sessionRootDir)
	w.list.UnselectAll()
	w.clearDetails()
	if err != nil {
		w.sessions = nil
		w.hint.SetText("加载失败：" + err.Error())
		w.list.Refresh()
		return
	}

	query := strings.TrimSpace(w.searchEntry.Text)
	var firstReadErr error
	failedReads := 0
	if query != "" {
		matches := make([]SessionInfo, 0, len(sessions))
		needle := strings.ToLower(query)
		for _, info := range sessions {
			if strings.Contains(strings.ToLower(info.Name), needle) {
				matches = append(matches, info)
				continue
			}
			if info.TranscriptFile == "" {
				continue
			}
			path := filepath.Join(info.Directory, filepath.Base(info.TranscriptFile))
			data, err := os.ReadFile(path)
			if err != nil {
				readErr := fmt.Errorf("读取会话 %q 的转写失败：%w", info.Name, err)
				log.Print(readErr)
				failedReads++
				if firstReadErr == nil {
					firstReadErr = readErr
				}
				continue
			}
			if strings.Contains(strings.ToLower(string(data)), needle) {
				matches = append(matches, info)
			}
		}
		sessions = matches
	}
	w.sessions = sessions

	switch {
	case query == "" && len(sessions) == 0:
		w.hint.SetText("暂无会话")
	case query == "":
		w.hint.SetText(fmt.Sprintf("共 %d 个会话；主目录：%s", len(sessions), w.setting.sessionRootDir))
	case len(sessions) == 0:
		w.hint.SetText(fmt.Sprintf("没有找到匹配“%s”的会话", query))
	default:
		w.hint.SetText(fmt.Sprintf("找到 %d 个匹配“%s”的会话", len(sessions), query))
	}
	if firstReadErr != nil {
		w.hint.SetText(fmt.Sprintf("%s；%d 个转写文件读取失败（%v）",
			w.hint.Text, failedReads, firstReadErr))
	}
	w.list.Refresh()
	if len(w.sessions) > 0 {
		w.list.Select(0)
	}
}

func (w *sessionHistoryWin) clearDetails() {
	w.selected = nil
	w.name.SetText("选择一个会话")
	w.deleteButton.Disable()
	w.metadata.SetText("")
	w.directory.SetText("")
	w.detailError.SetText("")
	w.recordings.Objects = nil
	w.recordings.Refresh()
	w.transcript.SetText("")
	w.details.Refresh()
}

func (w *sessionHistoryWin) showSession(info SessionInfo) {
	defer w.details.Refresh()

	selected := info
	w.selected = &selected
	w.deleteButton.Enable()
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
		play := widget.NewButtonWithIcon("播放", theme.MediaPlayIcon(), func() {
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
		if fileInfo, err := os.Stat(path); err == nil && fileInfo.Size() <= wavHeaderSize {
			label.SetText(fileName + "（无音频数据）")
			play.Disable()
		}
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

func (w *sessionHistoryWin) confirmDeleteSession() {
	if w.selected == nil {
		return
	}
	info := *w.selected
	dialog.ShowConfirm(
		"删除会话",
		fmt.Sprintf("确定删除会话“%s”及其录音和转写文件吗？此操作不可恢复。", info.Name),
		func(confirm bool) {
			if !confirm {
				return
			}
			if err := deleteSessionDirectory(w.setting.sessionRootDir, info); err != nil {
				dialog.ShowError(err, w.window)
				return
			}
			w.searchSessions()
			w.hint.SetText(fmt.Sprintf("已删除会话：%s", info.Name))
		},
		w.window,
	)
}

func deleteSessionDirectory(rootDir string, info SessionInfo) error {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("resolve session root: %w", err)
	}
	target, err := filepath.Abs(info.Directory)
	if err != nil {
		return fmt.Errorf("resolve session directory: %w", err)
	}
	if filepath.Dir(target) != root || filepath.Base(target) != info.Name || target == root {
		return fmt.Errorf("refusing to delete session outside its root directory")
	}

	entryInfo, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("inspect session directory: %w", err)
	}
	if !entryInfo.IsDir() {
		return fmt.Errorf("session path is not a directory")
	}

	data, err := os.ReadFile(filepath.Join(target, sessionManifestName))
	if err != nil {
		return fmt.Errorf("read session manifest before deletion: %w", err)
	}
	var manifest SessionInfo
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode session manifest before deletion: %w", err)
	}
	if manifest.Name != info.Name {
		return fmt.Errorf("session manifest name does not match selected session")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("delete session directory: %w", err)
	}
	return nil
}
