package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestLoadInterviewResume(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resume.md")
	if err := os.WriteFile(path, []byte("  Go 项目经验  "), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := loadInterviewResume(true, path); err != nil || got != "Go 项目经验" {
		t.Fatalf("resume = %q, err = %v", got, err)
	}
	if got, err := loadInterviewResume(false, filepath.Join(dir, "missing")); err != nil || got != "" {
		t.Fatalf("disabled resume = %q, err = %v", got, err)
	}
	for _, invalid := range []string{"", filepath.Join(dir, "missing.md"), filepath.Join(dir, "resume.txt")} {
		if _, err := loadInterviewResume(true, invalid); err == nil {
			t.Fatalf("expected error for resume path %q", invalid)
		}
	}
	for _, data := range [][]byte{
		[]byte(" \n "),
		{0xff},
		[]byte("binary\x00content"),
		[]byte(strings.Repeat("x", maxInterviewContextBytes+1)),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadInterviewResume(true, path); err == nil {
			t.Fatal("invalid resume should fail explicitly")
		}
	}
}

func TestInterviewJobDescriptionPersistenceAndHistory(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	root := t.TempDir()
	session, err := CreateSession(root, "后端职位", SessionKindInterview)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SaveJobDescription("  需要 Go 并发经验  "); err != nil {
		t.Fatal(err)
	}
	if err := session.Finish(SessionStatusStopped, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveJobDescription("不能更改"); err == nil {
		t.Fatal("finished session accepted job description")
	}
	sessions, err := LoadSessions(root)
	if err != nil || len(sessions) != 1 || sessions[0].JobDescriptionFile != sessionJobDescriptionName {
		t.Fatalf("job description was not recorded: %+v, %v", sessions, err)
	}
	win := &sessionHistoryWin{setting: &appSetting{Setting: Setting{sessionRootDir: root}}}
	win.window = app.NewWindow("history")
	defer win.window.Close()
	win.buildContent()
	win.showSession(sessions[0])
	if win.jobDescription.Text != "需要 Go 并发经验" || !win.jobDescription.Selectable ||
		len(win.historyTabs.Items) != 3 || win.historyTabs.Items[2] != win.jobTab {
		t.Fatal("saved job description is not selectable in interview history")
	}
	missing := sessions[0]
	missing.JobDescriptionFile = "missing.txt"
	win.showSession(missing)
	if !strings.Contains(win.detailError.Text, "职位描述") || win.jobDescription.Text != "" {
		t.Fatal("missing job description should display an error, not stale content")
	}
	legacy := sessions[0]
	legacy.JobDescriptionFile = ""
	win.showSession(legacy)
	if len(win.historyTabs.Items) != 2 || win.jobDescription.Text != "" {
		t.Fatal("old interviews without a job description should not show the job tab")
	}
	recording, err := CreateSession(root, "录音", SessionKindRecording)
	if err != nil {
		t.Fatal(err)
	}
	if err := recording.SaveJobDescription("职位"); err == nil {
		t.Fatal("recording accepted a job description")
	}
	win.showSession(recording.Snapshot())
	if win.historyTabs.Visible() || win.jobDescription.Text != "" {
		t.Fatal("job description leaked into another session")
	}
}

func TestInterviewJobDescriptionSizeLimit(t *testing.T) {
	session, err := CreateSession(t.TempDir(), "职位", SessionKindInterview)
	if err != nil {
		t.Fatal(err)
	}
	maximum := strings.Repeat("a", maxInterviewContextBytes)
	if err := session.SaveJobDescription(maximum); err != nil {
		t.Fatalf("maximum-sized job description rejected: %v", err)
	}
	loaded, err := readInterviewReference(filepath.Join(session.Directory(), sessionJobDescriptionName), "职位描述")
	if err != nil || loaded != maximum {
		t.Fatalf("maximum-sized description did not round-trip: length=%d err=%v", len(loaded), err)
	}
	if err := session.SaveJobDescription(maximum + "a"); err == nil {
		t.Fatal("oversized job description was accepted")
	}
}

func TestResumeSettingsPersist(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	setting := newAppSetting()
	window := app.NewWindow("home")
	defer window.Close()
	resume := setting.makeInterviewReferencePanel(window).Objects[0]
	row := resume.(*fyne.Container).Objects[0].(*fyne.Container)
	check := findCheck(row)
	if check == nil || check.Checked || check.Text != "使用简历" {
		t.Fatal("resume must retain its label and be initially disabled")
	}
	fynetest.Tap(check)
	if !setting.resumeEnabled || !app.Preferences().Bool(resumeEnabledPreference) {
		t.Fatal("resume checkbox did not persist enabled state")
	}
	var entry *widget.Entry
	for _, obj := range row.Objects {
		if value, ok := obj.(*widget.Entry); ok {
			entry = value
		}
	}
	if entry == nil || entry.Disabled() {
		t.Fatal("resume path entry was not enabled")
	}
	entry.SetText("example.md")
	if setting.resumePath != "example.md" || app.Preferences().String(resumePathPreference) != "example.md" {
		t.Fatal("resume path was not persisted")
	}
	restored := newAppSetting()
	restored.loadInterviewReferencePreferences()
	if !restored.resumeEnabled || restored.resumePath != "example.md" {
		t.Fatal("resume settings were not restored")
	}
	fynetest.Tap(check)
	if setting.resumeEnabled || !entry.Disabled() {
		t.Fatal("disabled resume should disable path entry")
	}
}

func TestInterviewSetupHasNameAndJobDescription(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	showInterviewSetup()
	activeInterview.Lock()
	window := activeInterview.setup
	activeInterview.Unlock()
	if window == nil {
		t.Fatal("interview setup window not created")
	}
	defer closeInterviewSetup()
	var entries []*widget.Entry
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		if entry, ok := object.(*widget.Entry); ok {
			entries = append(entries, entry)
		}
		if group, ok := object.(*fyne.Container); ok {
			for _, child := range group.Objects {
				visit(child)
			}
		}
	}
	visit(window.Content())
	if len(entries) != 2 || entries[0].MultiLine == entries[1].MultiLine {
		t.Fatalf("setup should contain title and multiline job description, got %d entries", len(entries))
	}
	closeInterviewSetup()
	activeInterview.Lock()
	defer activeInterview.Unlock()
	if activeInterview.setup != nil {
		t.Fatal("closed setup window remained active")
	}
}
