package sound_cap

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const maxWAVDataBytes = uint64(math.MaxUint32 - 36)

// wavWriter streams PCM instead of retaining a whole recording in memory.
type wavWriter struct {
	file      *os.File
	dataBytes uint64
}

func newWAVWriter(path string, sampleRate, channels, bitsPerSample int) (*wavWriter, error) {
	if sampleRate <= 0 || channels <= 0 || bitsPerSample != 16 {
		return nil, fmt.Errorf("unsupported WAV format: rate=%d channels=%d bits=%d", sampleRate, channels, bitsPerSample)
	}
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return nil, fmt.Errorf("create recording directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create recording %q: %w", path, err)
	}
	w := &wavWriter{file: file}
	if err := w.writeHeader(sampleRate, channels, bitsPerSample); err != nil {
		_ = file.Close()
		return nil, err
	}
	return w, nil
}

func (w *wavWriter) writeHeader(sampleRate, channels, bitsPerSample int) error {
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36) // Updated by Close.
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(header[34:36], uint16(bitsPerSample))
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], 0) // Updated by Close.
	if _, err := w.file.Write(header); err != nil {
		return fmt.Errorf("write WAV header: %w", err)
	}
	return nil
}

func (w *wavWriter) Write(p []byte) (int, error) {
	if w == nil || w.file == nil {
		return 0, os.ErrClosed
	}
	if uint64(len(p)) > maxWAVDataBytes-w.dataBytes {
		return 0, fmt.Errorf("WAV recording exceeds RIFF 4 GiB limit")
	}
	n, err := w.file.Write(p)
	w.dataBytes += uint64(n)
	return n, err
}

func (w *wavWriter) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	file := w.file
	w.file = nil
	if w.dataBytes > maxWAVDataBytes {
		_ = file.Close()
		return fmt.Errorf("WAV recording exceeds RIFF size limit")
	}
	if _, err := file.Seek(4, 0); err != nil {
		_ = file.Close()
		return fmt.Errorf("seek WAV RIFF size: %w", err)
	}
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(36+w.dataBytes))
	if _, err := file.Write(size[:]); err != nil {
		_ = file.Close()
		return fmt.Errorf("finalize WAV RIFF size: %w", err)
	}
	if _, err := file.Seek(40, 0); err != nil {
		_ = file.Close()
		return fmt.Errorf("seek WAV data size: %w", err)
	}
	binary.LittleEndian.PutUint32(size[:], uint32(w.dataBytes))
	if _, err := file.Write(size[:]); err != nil {
		_ = file.Close()
		return fmt.Errorf("finalize WAV data size: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close WAV recording: %w", err)
	}
	return nil
}
