//go:build linux

package ffaudio

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPulseDeviceAndLoopbackNative(t *testing.T) {
	flags, err := exec.Command("pkg-config", "--cflags", "--libs", "libpulse").Output()
	if err != nil {
		t.Fatalf("find PulseAudio build flags: %v", err)
	}
	compiler := os.Getenv("CC")
	if compiler == "" {
		compiler = "cc"
	}
	executable := filepath.Join(t.TempDir(), "pulse-test")
	args := strings.Fields(os.Getenv("CGO_CFLAGS"))
	args = append(args, "-std=gnu99", "-DFFAUDIO_INTERFACE=test_interface", "-I.",
		filepath.Join("testdata", "pulse_test.c"), "-o", executable)
	args = append(args, strings.Fields(os.Getenv("CGO_LDFLAGS"))...)
	args = append(args, strings.Fields(string(flags))...)
	if output, err := exec.Command(compiler, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile PulseAudio test: %v\n%s", err, output)
	}
	if output, err := exec.Command(executable).CombinedOutput(); err != nil {
		t.Fatalf("PulseAudio test: %v\n%s", err, output)
	}
}

func TestPulseConnectionFailure(t *testing.T) {
	t.Setenv("PULSE_SERVER", "unix:"+filepath.Join(t.TempDir(), "missing"))
	if err := Init(); err == nil {
		UnInit()
		t.Fatal("Init unexpectedly connected to a nonexistent server")
	}
}
