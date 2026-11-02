package gui

import (
	"os"
	"path/filepath"
	"testing"

	"xaux_gui/pkg/sound_cap"

	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestUnselectedRecordSourceCannotStart(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	source := newRecordSource(
		&RecorderWin{},
		"microphone",
		"test device",
		sound_cap.CaptureMicrophone,
		true,
		false,
	)
	path := filepath.Join(t.TempDir(), "unused.wav")
	if err := source.start(path); err != nil {
		t.Fatal(err)
	}
	if source.currentCapture() != nil {
		t.Fatal("unselected source created a capture")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unselected source created an output file: %v", err)
	}
}

func TestRecorderSessionGroupsSourcesAndSequencesNames(t *testing.T) {
	win := &RecorderWin{
		saveDir: t.TempDir(),
		sources: []*recordSource{
			{title: "microphone", isMicrophone: true, selected: true},
			{title: "speaker", selected: true},
		},
	}
	for _, expectedName := range []string{"recording", "recording-2", "recording-3"} {
		session, paths, err := win.prepareSession("recording")
		if err != nil {
			t.Fatal(err)
		}
		info := session.Snapshot()
		if info.Name != expectedName {
			t.Fatalf("session name = %q, want %q", info.Name, expectedName)
		}
		if len(paths) != 2 || len(info.Recordings) != 2 {
			t.Fatalf("session has %d paths and %d manifest recordings", len(paths), len(info.Recordings))
		}
		for source, path := range paths {
			if filepath.Dir(path) != info.Directory {
				t.Fatalf("%s output is outside the shared session directory", source.title)
			}
		}
		if err := session.Finish(SessionStatusStopped, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecorderRestoresRequestedSessionName(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	for _, requestedName := range []string{"recording", ""} {
		t.Run(requestedName, func(t *testing.T) {
			session, err := CreateSession(t.TempDir(), requestedName, SessionKindRecording)
			if err != nil {
				t.Fatal(err)
			}
			win := &RecorderWin{
				session:       session,
				requestedName: requestedName,
				sessionName:   widget.NewEntry(),
				errorLabel:    widget.NewLabel(""),
				startButton:   widget.NewButton("start", nil),
				stopButton:    widget.NewButton("stop", nil),
			}
			win.sessionName.SetText(session.Snapshot().Name)
			win.finishSession()
			if win.sessionName.Text != requestedName {
				t.Fatalf("restored name = %q, want %q", win.sessionName.Text, requestedName)
			}
		})
	}
}
