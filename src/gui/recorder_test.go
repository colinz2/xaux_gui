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

func TestRecorderLayoutEmphasizesControlsWithoutEmptyErrorSpace(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	win := &RecorderWin{
		saveDir:       t.TempDir(),
		sessionName:   widget.NewEntry(),
		durationLabel: widget.NewLabel("时长：--:--:--"),
		errorLabel:    widget.NewLabel(""),
	}
	win.setError("")
	win.sources = []*recordSource{
		newRecordSource(win, "麦克风", "测试麦克风", sound_cap.CaptureMicrophone, true, true),
		newRecordSource(win, "扬声器", "测试扬声器", sound_cap.CaptureSpeaker, false, true),
	}
	win.makeContent()

	if win.startButton.Text != "开始录音" || win.startButton.Importance != widget.HighImportance {
		t.Fatalf("start button = %q with importance %v", win.startButton.Text, win.startButton.Importance)
	}
	if win.stopButton.Text != "停止录音" || win.stopButton.Importance != widget.DangerImportance {
		t.Fatalf("stop button = %q with importance %v", win.stopButton.Text, win.stopButton.Importance)
	}
	if win.errorLabel.Visible() {
		t.Fatal("empty recorder error label should not take up layout space")
	}
	for _, source := range win.sources {
		if source.errorLabel.Visible() {
			t.Fatalf("empty %s error label should not take up layout space", source.title)
		}
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
