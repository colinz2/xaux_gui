package gui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenSessionRecording(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "面试 #1 50%")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	recording := filepath.Join(dir, "麦克风 & 扬声器.wav")
	if err := os.WriteFile(recording, make([]byte, wavHeaderSize+2), 0o600); err != nil {
		t.Fatal(err)
	}
	var opened string
	opener := func(path string) error {
		opened = path
		return nil
	}
	if err := openSessionRecording(recording, opener); err != nil {
		t.Fatal(err)
	}
	if opened != recording {
		t.Fatalf("opened path = %q, want %q", opened, recording)
	}

	empty := filepath.Join(dir, "empty.wav")
	if err := os.WriteFile(empty, make([]byte, wavHeaderSize), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{empty, dir, filepath.Join(dir, "missing.wav")} {
		opened = ""
		if err := openSessionRecording(path, opener); err == nil || opened != "" {
			t.Fatalf("path %q: expected an error and no player launch", path)
		}
	}
	playerErr := errors.New("no file association")
	err := openSessionRecording(recording, func(string) error { return playerErr })
	if !errors.Is(err, playerErr) || !strings.Contains(err.Error(), "默认播放器") {
		t.Fatalf("player error was not surfaced: %v", err)
	}
}
