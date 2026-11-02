package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	fynetest "fyne.io/fyne/v2/test"
)

func TestSessionUniqueNamesAndPersistence(t *testing.T) {
	root := t.TempDir()
	first, err := CreateSession(root, "recording", SessionKindRecording)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateSession(root, "recording", SessionKindRecording)
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot().Name != "recording" || second.Snapshot().Name != "recording-2" {
		t.Fatalf("unexpected unique names: %q, %q", first.Snapshot().Name, second.Snapshot().Name)
	}
	if first.Directory() == second.Directory() {
		t.Fatal("sessions share a directory")
	}

	if err := first.AddRecording("microphone.wav"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.Directory(), "microphone.wav"), []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := first.Finish(SessionStatusStopped, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := second.Finish(SessionStatusFailed, nil, errors.New("capture failed")); err != nil {
		t.Fatal(err)
	}

	sessions, err := LoadSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("loaded %d sessions, want 2", len(sessions))
	}
	byName := make(map[string]SessionInfo, len(sessions))
	for _, info := range sessions {
		byName[info.Name] = info
	}
	stopped := byName[first.Snapshot().Name]
	if stopped.Status != SessionStatusStopped || len(stopped.Recordings) != 1 || stopped.Recordings[0] != "microphone.wav" {
		t.Fatalf("recording metadata was not persisted: %+v", stopped)
	}
	failed := byName[second.Snapshot().Name]
	if failed.Status != SessionStatusFailed || failed.Error != "capture failed" {
		t.Fatalf("failure metadata was not persisted: %+v", failed)
	}
}

func TestSessionTranscriptAndDefaultName(t *testing.T) {
	root := t.TempDir()
	session, err := CreateSession(root, ` ../invalid:name `, SessionKindTranscription)
	if err != nil {
		t.Fatal(err)
	}
	name := session.Snapshot().Name
	if name == "" || strings.ContainsAny(name, `/\\:`) {
		t.Fatalf("unsafe session name %q", name)
	}
	entries := []SessionTranscriptEntry{
		{Timestamp: "2026-01-02 03:04:05", Device: "麦克风", Content: "第一段"},
		{Timestamp: "2026-01-02 03:04:08", Device: "扬声器", Content: "第二段"},
	}
	if err := session.Finish(SessionStatusStopped, entries, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(session.Directory(), sessionTranscriptName))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"麦克风", "第一段", "扬声器", "第二段"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("transcript does not contain %q: %s", expected, text)
		}
	}
	sessions, err := LoadSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].TranscriptFile != sessionTranscriptName {
		t.Fatalf("transcript metadata was not loaded: %+v", sessions)
	}
}

func TestSessionHistoryWindow(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()
	root := t.TempDir()
	session, err := CreateSession(root, "历史测试", SessionKindTranscription)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AddRecording("microphone.wav"); err != nil {
		t.Fatal(err)
	}
	if err := session.AddRecording("speaker.wav"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session.Directory(), "microphone.wav"), []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session.Directory(), "speaker.wav"), []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := session.Finish(SessionStatusStopped, []SessionTranscriptEntry{
		{Timestamp: "2026-01-02 03:04:05", Device: "麦克风", Content: "历史转写内容"},
	}, nil); err != nil {
		t.Fatal(err)
	}

	win := &sessionHistoryWin{setting: &appSetting{Setting: Setting{sessionRootDir: root}}}
	win.window = app.NewWindow("test")
	defer win.window.Close()
	win.buildContent()
	win.refresh()
	win.window.Show()
	if len(win.sessions) != 1 {
		t.Fatalf("history contains %d sessions, want 1", len(win.sessions))
	}
	win.list.Select(0)
	if win.name.Text != "历史测试" || len(win.recordings.Objects) != 2 {
		t.Fatalf("history details were not populated: %q, %d recordings", win.name.Text, len(win.recordings.Objects))
	}
	if !strings.Contains(win.transcript.Text, "历史转写内容") {
		t.Fatalf("history transcript was not loaded: %q", win.transcript.Text)
	}
	transcriptTitle := win.details.Objects[8]
	if transcriptTitle.Position().Y < win.recordings.Position().Y+win.recordings.Size().Height {
		t.Fatalf("transcript section overlaps recordings: recordings at %v size %v, transcript at %v", win.recordings.Position(), win.recordings.Size(), transcriptTitle.Position())
	}
	if !strings.Contains(win.metadata.Text, "持续时长：00:00:00") {
		t.Fatalf("history duration was not displayed: %q", win.metadata.Text)
	}
}

func TestSessionDefaultNameAndConcurrentUniqueness(t *testing.T) {
	if got := normalizeSessionName("", time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)); got != "20260102-030405" {
		t.Fatalf("unexpected default name %q", got)
	}

	root := t.TempDir()
	const count = 8
	names := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session, err := CreateSession(root, "same", SessionKindRecording)
			if err != nil {
				errs <- err
				return
			}
			names <- session.Snapshot().Name
		}()
	}
	wg.Wait()
	close(names)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	unique := make(map[string]struct{}, count)
	for name := range names {
		if _, exists := unique[name]; exists {
			t.Errorf("duplicate session name %q", name)
		}
		unique[name] = struct{}{}
	}
	if len(unique) != count {
		t.Fatalf("created %d unique sessions, want %d: %s", len(unique), count, fmt.Sprint(unique))
	}
}
