package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func writeKnowledgeFile(t *testing.T, directory, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadInterviewKnowledgeMultipleFiles(t *testing.T) {
	directory := t.TempDir()
	writeKnowledgeFile(t, directory, "b.TXT", "并发：注意数据竞争")
	writeKnowledgeFile(t, directory, "a.md", "回答思路：先说结论")
	writeKnowledgeFile(t, directory, "ignored.pdf", "不读取")
	if err := os.Mkdir(filepath.Join(directory, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeKnowledgeFile(t, filepath.Join(directory, "nested"), "nested.md", "不递归")
	got, err := loadInterviewKnowledge(true, directory)
	if err != nil {
		t.Fatal(err)
	}
	if got != "回答思路：先说结论\n\n---\n\n并发：注意数据竞争" {
		t.Fatalf("unexpected knowledge: %q", got)
	}
	if disabled, err := loadInterviewKnowledge(false, "does-not-exist"); err != nil || disabled != "" {
		t.Fatalf("disabled knowledge: %q, %v", disabled, err)
	}
}

func TestLoadInterviewKnowledgeReportsInvalidInputs(t *testing.T) {
	for _, directory := range []string{"", filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if _, err := loadInterviewKnowledge(true, directory); err == nil {
			t.Fatalf("expected error for %q", directory)
		}
	}
	for _, content := range []string{"", " ", string([]byte{0xff}), "binary\x00data", strings.Repeat("x", maxInterviewContextBytes+1)} {
		directory := t.TempDir()
		writeKnowledgeFile(t, directory, "invalid.md", content)
		if _, err := loadInterviewKnowledge(true, directory); err == nil {
			t.Fatalf("invalid content accepted, bytes=%d", len(content))
		}
	}
}

func TestLoadInterviewKnowledgeLimitsCombinedSizeAndFileCount(t *testing.T) {
	directory := t.TempDir()
	writeKnowledgeFile(t, directory, "a.md", strings.Repeat("a", maxInterviewContextBytes-8))
	writeKnowledgeFile(t, directory, "b.md", "b")
	result, err := loadInterviewKnowledge(true, directory)
	if err != nil || len(result) != maxInterviewContextBytes {
		t.Fatalf("maximum-sized knowledge rejected: bytes=%d err=%v", len(result), err)
	}
	writeKnowledgeFile(t, directory, "b.md", "bb")
	if _, err := loadInterviewKnowledge(true, directory); err == nil {
		t.Fatal("knowledge above the combined byte limit was accepted")
	}

	directory = t.TempDir()
	for i := 0; i < maxKnowledgeFiles; i++ {
		writeKnowledgeFile(t, directory, fmt.Sprintf("%03d.md", i), "知识")
	}
	if _, err := loadInterviewKnowledge(true, directory); err != nil {
		t.Fatalf("maximum number of files rejected: %v", err)
	}
	writeKnowledgeFile(t, directory, "extra.md", "额外知识")
	if _, err := loadInterviewKnowledge(true, directory); err == nil {
		t.Fatal("too many knowledge files accepted")
	}
}

func TestKnowledgePanelPersistsAndRestoresConfiguration(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	setting := newAppSetting()
	window := app.NewWindow("home")
	defer window.Close()
	panel := setting.makeInterviewReferencePanel(window).Objects[2].(*fyne.Container)
	row := panel.Objects[0].(*fyne.Container)
	check := findCheck(row)
	if check == nil || check.Checked || check.Text != "面试知识" {
		t.Fatal("knowledge should be labeled 面试知识 and disabled by default")
	}
	var entry *widget.Entry
	for _, object := range row.Objects {
		if value, ok := object.(*widget.Entry); ok {
			entry = value
		}
	}
	if entry == nil || !entry.Disabled() {
		t.Fatal("disabled knowledge should disable its path")
	}
	fynetest.Tap(check)
	entry.SetText("knowledge-folder")
	restored := newAppSetting()
	restored.loadInterviewReferencePreferences()
	if !restored.knowledgeEnabled || restored.knowledgePath != "knowledge-folder" {
		t.Fatal("knowledge configuration did not persist")
	}
	fynetest.Tap(check)
	if setting.knowledgeEnabled || !entry.Disabled() {
		t.Fatal("disabling knowledge should disable its path")
	}
}
