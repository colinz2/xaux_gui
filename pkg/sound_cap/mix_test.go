package sound_cap

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func writeMixTestWAV(t *testing.T, path string, samples []int16) {
	t.Helper()
	writer, err := newWAVWriter(path, 16000, 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func readMixTestWAV(t *testing.T, path string) []int16 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 44 || string(data[:4]) != "RIFF" ||
		binary.LittleEndian.Uint16(data[22:24]) != 2 ||
		binary.LittleEndian.Uint32(data[24:28]) != 16000 ||
		int(binary.LittleEndian.Uint32(data[40:44])) != len(data)-44 {
		t.Fatalf("invalid stereo WAV header: %v", data)
	}
	var result []int16
	for i := 44; i < len(data); i += 4 {
		left := int16(binary.LittleEndian.Uint16(data[i : i+2]))
		right := int16(binary.LittleEndian.Uint16(data[i+2 : i+4]))
		if left != right {
			t.Fatalf("channels differ: %d != %d", left, right)
		}
		result = append(result, left)
	}
	return result
}

func TestMixRecordingsAlignsSourcesAndKeepsOriginals(t *testing.T) {
	dir := t.TempDir()
	mic := filepath.Join(dir, "mic.wav")
	speaker := filepath.Join(dir, "speaker.wav")
	output := filepath.Join(dir, "mixed.wav")
	writeMixTestWAV(t, mic, []int16{1000, 2000, 3000, 4000})
	writeMixTestWAV(t, speaker, []int16{100, 200, 300})
	micBefore, _ := os.ReadFile(mic)
	speakerBefore, _ := os.ReadFile(speaker)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	err := MixRecordings(output, []RecordingInput{
		{Path: mic, Segments: []RecordingSegment{{StartedAt: start}}},
		{Path: speaker, Segments: []RecordingSegment{{StartedAt: start.Add(2 * time.Second / 16000)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readMixTestWAV(t, output), []int16{1000, 2000, 1550, 2100, 300}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed samples = %v, want %v", got, want)
	}
	micAfter, _ := os.ReadFile(mic)
	speakerAfter, _ := os.ReadFile(speaker)
	if !reflect.DeepEqual(micBefore, micAfter) || !reflect.DeepEqual(speakerBefore, speakerAfter) {
		t.Fatal("original recordings changed")
	}
}

func TestMixRecordingsHandlesPausesAndClipping(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.wav"), filepath.Join(dir, "b.wav")
	writeMixTestWAV(t, a, []int16{32767, -32768, 1000, 2000})
	writeMixTestWAV(t, b, []int16{32767, -32768, 3000, 4000})
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	segments := []RecordingSegment{{StartedAt: start}, {Frame: 2, StartedAt: start.Add(10 * time.Second)}}
	output := filepath.Join(dir, "mixed.wav")
	if err := MixRecordings(output, []RecordingInput{{Path: a, Segments: segments}, {Path: b, Segments: segments}}); err != nil {
		t.Fatal(err)
	}
	if got, want := readMixTestWAV(t, output), []int16{32767, -32768, 2000, 3000}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed samples = %v, want %v", got, want)
	}
}

func TestMixRecordingsHandlesOneOrEmptySource(t *testing.T) {
	dir := t.TempDir()
	a, empty := filepath.Join(dir, "a.wav"), filepath.Join(dir, "empty.wav")
	writeMixTestWAV(t, a, []int16{100, -200})
	writeMixTestWAV(t, empty, nil)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, inputs := range [][]RecordingInput{
		{{Path: a, Segments: []RecordingSegment{{StartedAt: start}}}},
		{{Path: a, Segments: []RecordingSegment{{StartedAt: start}}}, {Path: empty}},
	} {
		output := filepath.Join(dir, "mixed.wav")
		if err := MixRecordings(output, inputs); err != nil {
			t.Fatal(err)
		}
		if got, want := readMixTestWAV(t, output), []int16{100, -200}; !reflect.DeepEqual(got, want) {
			t.Fatalf("mixed samples = %v, want %v", got, want)
		}
		if err := os.Remove(output); err != nil {
			t.Fatal(err)
		}
	}
	if err := MixRecordings(filepath.Join(dir, "mixed.wav"), []RecordingInput{{Path: empty}}); err == nil {
		t.Fatal("empty audio was accepted")
	}
}

func TestMixRecordingsReportsInvalidInputsWithoutOverwriting(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "source.wav"), filepath.Join(dir, "mixed.wav")
	writeMixTestWAV(t, input, []int16{100})
	if err := MixRecordings(output, []RecordingInput{{Path: input}}); err == nil {
		t.Fatal("missing timing was accepted")
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed mix left output: %v", err)
	}
	if err := os.WriteFile(output, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := MixRecordings(output, []RecordingInput{{Path: input, Segments: []RecordingSegment{{StartedAt: start}}}}); err == nil {
		t.Fatal("existing output was overwritten")
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "keep" {
		t.Fatalf("existing output changed: %q, %v", data, err)
	}
}
