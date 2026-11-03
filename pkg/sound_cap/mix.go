package sound_cap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type RecordingInput struct {
	Path     string
	Segments []RecordingSegment
}

type mixSegment struct {
	source      int
	sourceFrame uint64
	start       int64
	end         int64
	outputStart int64
}

type mixSource struct {
	file   *os.File
	frames uint64
}

// MixRecordings combines up to two mono PCM recordings into a stereo WAV.
// The segment timestamps align independently started captures and remove gaps
// where every capture was paused.
func MixRecordings(outputPath string, inputs []RecordingInput) error {
	if len(inputs) == 0 || len(inputs) > 2 {
		return fmt.Errorf("expected one or two recordings, got %d", len(inputs))
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("mixed recording already exists: %q", outputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check mixed recording path: %w", err)
	}
	sources := make([]mixSource, len(inputs))
	defer func() {
		for _, source := range sources {
			if source.file != nil {
				_ = source.file.Close()
			}
		}
	}()
	sampleRate := uint32(0)
	var origin time.Time
	for i, input := range inputs {
		file, err := os.Open(input.Path)
		if err != nil {
			return fmt.Errorf("open recording %q: %w", input.Path, err)
		}
		sources[i].file = file
		info, err := file.Stat()
		if err != nil {
			return fmt.Errorf("stat recording %q: %w", input.Path, err)
		}
		var header [44]byte
		if _, err := io.ReadFull(file, header[:]); err != nil {
			return fmt.Errorf("read WAV header %q: %w", input.Path, err)
		}
		rate := binary.LittleEndian.Uint32(header[24:28])
		dataSize := binary.LittleEndian.Uint32(header[40:44])
		if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" ||
			string(header[12:16]) != "fmt " || binary.LittleEndian.Uint32(header[16:20]) != 16 ||
			binary.LittleEndian.Uint16(header[20:22]) != 1 || binary.LittleEndian.Uint16(header[22:24]) != 1 ||
			rate == 0 || binary.LittleEndian.Uint16(header[32:34]) != 2 ||
			binary.LittleEndian.Uint16(header[34:36]) != 16 || string(header[36:40]) != "data" ||
			dataSize%2 != 0 || uint64(info.Size()) != uint64(44)+uint64(dataSize) ||
			uint64(binary.LittleEndian.Uint32(header[4:8])) != 36+uint64(dataSize) ||
			uint64(binary.LittleEndian.Uint32(header[28:32])) != uint64(rate)*2 {
			return fmt.Errorf("unsupported or incomplete mono PCM WAV: %q", input.Path)
		}
		if sampleRate != 0 && sampleRate != rate {
			return fmt.Errorf("recording sample rates differ: %d and %d", sampleRate, rate)
		}
		sampleRate = rate
		sources[i].frames = uint64(dataSize / 2)
		if sources[i].frames == 0 {
			if len(input.Segments) != 0 {
				return fmt.Errorf("empty recording has segment timestamps: %q", input.Path)
			}
			continue
		}
		if len(input.Segments) == 0 || input.Segments[0].Frame != 0 {
			return fmt.Errorf("recording timing is unavailable: %q", input.Path)
		}
		for j, segment := range input.Segments {
			if segment.StartedAt.IsZero() || segment.Frame >= sources[i].frames ||
				(j > 0 && (segment.Frame <= input.Segments[j-1].Frame || segment.StartedAt.Before(input.Segments[j-1].StartedAt))) {
				return fmt.Errorf("invalid recording segment timing: %q", input.Path)
			}
			if origin.IsZero() || segment.StartedAt.Before(origin) {
				origin = segment.StartedAt
			}
		}
	}
	if origin.IsZero() {
		return errors.New("recordings contain no audio data")
	}

	var segments []mixSegment
	for i, input := range inputs {
		var previousEnd int64
		for j, segment := range input.Segments {
			endFrame := sources[i].frames
			if j+1 < len(input.Segments) {
				endFrame = input.Segments[j+1].Frame
			}
			elapsed := segment.StartedAt.Sub(origin)
			if int64(elapsed/time.Second) > (math.MaxInt64-int64(sampleRate))/int64(sampleRate) {
				return errors.New("recording timeline exceeds supported duration")
			}
			start := int64(elapsed/time.Second)*int64(sampleRate) +
				int64(elapsed%time.Second)*int64(sampleRate)/int64(time.Second)
			if start < previousEnd {
				start = previousEnd
			}
			length := int64(endFrame - segment.Frame)
			if start > math.MaxInt64-length {
				return errors.New("recording timeline exceeds supported duration")
			}
			previousEnd = start + length
			segments = append(segments, mixSegment{source: i, sourceFrame: segment.Frame, start: start, end: previousEnd})
		}
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].start < segments[j].start })
	var outputFrames, groupStart, groupEnd, groupOutput int64
	for i := range segments {
		segment := &segments[i]
		if i == 0 || segment.start > groupEnd {
			if i != 0 {
				outputFrames += groupEnd - groupStart
			}
			groupStart, groupEnd, groupOutput = segment.start, segment.end, outputFrames
		} else if segment.end > groupEnd {
			groupEnd = segment.end
		}
		segment.outputStart = groupOutput + segment.start - groupStart
	}
	outputFrames += groupEnd - groupStart
	if outputFrames <= 0 || uint64(outputFrames) > maxWAVDataBytes/4 {
		return errors.New("mixed recording exceeds WAV size limit")
	}

	temp, err := os.CreateTemp(filepath.Dir(outputPath), ".mixed-*.wav")
	if err != nil {
		return fmt.Errorf("create temporary mixed recording: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary mixed recording: %w", err)
	}
	writer, err := newWAVWriter(tempPath, int(sampleRate), 2, 16)
	if err != nil {
		return err
	}
	const blockFrames = 8192
	buffers := make([][]byte, len(inputs))
	active := make([][]bool, len(inputs))
	for i := range buffers {
		buffers[i] = make([]byte, blockFrames*2)
		active[i] = make([]bool, blockFrames)
	}
	mixed := make([]byte, blockFrames*4)
	for blockStart := int64(0); blockStart < outputFrames; blockStart += blockFrames {
		count := min(int64(blockFrames), outputFrames-blockStart)
		for _, buffer := range buffers {
			clear(buffer[:count*2])
		}
		for _, frames := range active {
			clear(frames[:count])
		}
		for _, segment := range segments {
			segmentEnd := segment.outputStart + segment.end - segment.start
			start := max(blockStart, segment.outputStart)
			end := min(blockStart+count, segmentEnd)
			if start >= end {
				continue
			}
			destination := buffers[segment.source][(start-blockStart)*2 : (end-blockStart)*2]
			offset := int64(44) + int64(segment.sourceFrame)*2 + (start-segment.outputStart)*2
			if _, err := sources[segment.source].file.ReadAt(destination, offset); err != nil {
				_ = writer.Close()
				return fmt.Errorf("read recording audio: %w", err)
			}
			for i := start - blockStart; i < end-blockStart; i++ {
				active[segment.source][i] = true
			}
		}
		for i := int64(0); i < count; i++ {
			var sample int32
			activeCount := int32(0)
			for source, buffer := range buffers {
				if active[source][i] {
					activeCount++
					sample += int32(int16(binary.LittleEndian.Uint16(buffer[i*2 : i*2+2])))
				}
			}
			if activeCount > 0 {
				sample /= activeCount
			}
			binary.LittleEndian.PutUint16(mixed[i*4:i*4+2], uint16(int16(sample)))
			binary.LittleEndian.PutUint16(mixed[i*4+2:i*4+4], uint16(int16(sample)))
		}
		if n, err := writer.Write(mixed[:count*4]); err != nil || n != int(count*4) {
			_ = writer.Close()
			if err == nil {
				err = io.ErrShortWrite
			}
			return fmt.Errorf("write mixed recording: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, outputPath); err != nil {
		return fmt.Errorf("save mixed recording: %w", err)
	}
	return nil
}
