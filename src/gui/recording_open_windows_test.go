//go:build windows

package gui

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestOpenWindowsRecordingFileUsesNativePathAndReportsErrors(t *testing.T) {
	path := `C:\会话目录\面试 #1 50%\麦克风 & 扬声器.wav`
	wantErr := errors.New("player launch failed")
	called := false
	err := openWindowsRecordingFile(path, func(hwnd windows.Handle, verb, file, args, dir *uint16, show int32) error {
		called = true
		if hwnd != 0 || windows.UTF16PtrToString(verb) != "open" ||
			windows.UTF16PtrToString(file) != path || args != nil || dir != nil || show != windows.SW_SHOWNORMAL {
			t.Error("ShellExecute did not receive the unescaped native file path and open verb")
		}

		return wantErr
	})
	if !called || !errors.Is(err, wantErr) {
		t.Fatalf("called = %t, error = %v; want ShellExecute error", called, err)
	}
	called = false
	err = openWindowsRecordingFile("invalid\x00path", func(windows.Handle, *uint16, *uint16, *uint16, *uint16, int32) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatal("invalid path should fail before ShellExecute")
	}
}

func TestOpenWindowsRecordingWithSystemPlayer(t *testing.T) {
	if os.Getenv("XAUX_TEST_SYSTEM_PLAYER") != "1" {
		t.Skip("set XAUX_TEST_SYSTEM_PLAYER=1 to open a silent WAV in the system player")
	}
	dir := filepath.Join(t.TempDir(), "面试 测试")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "无声 录音.wav")
	data := make([]byte, wavHeaderSize+16000*2)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:16], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], 1)
	binary.LittleEndian.PutUint32(data[24:28], 16000)
	binary.LittleEndian.PutUint32(data[28:32], 32000)
	binary.LittleEndian.PutUint16(data[32:34], 2)
	binary.LittleEndian.PutUint16(data[34:36], 16)
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(len(data)-wavHeaderSize))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := openSessionRecording(path, openRecordingFile); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
}
