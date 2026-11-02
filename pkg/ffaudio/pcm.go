package ffaudio

import (
	"encoding/binary"
	"fmt"
	"math"
)

// PCMFormat describes interleaved PCM samples.
type PCMFormat struct {
	SampleRate int
	Channels   int
	Format     int
}

func (f PCMFormat) bytesPerSample() int {
	return (int(f.Format) & 0xff) / 8
}

func (f PCMFormat) frameSize() int {
	return f.bytesPerSample() * f.Channels
}

// PCMConverter converts native device PCM to interleaved signed 16-bit PCM.
// It downmixes by averaging channels and performs streaming linear resampling.
// The converter is stateful and must be called from one audio stream at a time.
type PCMConverter struct {
	in  PCMFormat
	out PCMFormat

	pending  []byte
	previous []int16
	phase    int64
}

func validPCMFormat(f PCMFormat) bool {
	if f.SampleRate <= 0 || f.Channels <= 0 || f.Channels > 32 {
		return false
	}
	switch f.Format {
	case FormatInt8, FormatUint8, FormatInt16, FormatInt24,
		FormatInt24_4, FormatInt32, FormatFloat32, FormatFloat64:
		return true
	default:
		return false
	}
}

func NewPCMConverter(in, out PCMFormat) (*PCMConverter, error) {
	if !validPCMFormat(in) {
		return nil, fmt.Errorf("invalid input PCM format: %+v", in)
	}
	if !validPCMFormat(out) || out.Format != FormatInt16 {
		return nil, fmt.Errorf("output PCM must be signed int16: %+v", out)
	}
	return &PCMConverter{in: in, out: out}, nil
}

func floatToInt16(v float64) int16 {
	if math.IsNaN(v) {
		return 0
	}
	if v <= -1 {
		return -32768
	}
	if v >= 1 {
		return 32767
	}
	return int16(math.Round(v * 32767))
}

func decodeInt16Sample(data []byte, format int) int16 {
	switch format {
	case FormatInt8:
		return int16(int8(data[0])) << 8
	case FormatUint8:
		return int16(int(data[0])-128) << 8
	case FormatInt16:
		return int16(binary.LittleEndian.Uint16(data))
	case FormatInt24:
		v := int32(data[0]) | int32(data[1])<<8 | int32(data[2])<<16
		if v&0x800000 != 0 {
			v |= ^0xffffff
		}
		return int16(v >> 8)
	case FormatInt24_4:
		// WASAPI stores 24 valid bits left-aligned in a 32-bit container.
		return int16(int32(binary.LittleEndian.Uint32(data)) >> 16)
	case FormatInt32:
		return int16(int32(binary.LittleEndian.Uint32(data)) >> 16)
	case FormatFloat32:
		return floatToInt16(float64(math.Float32frombits(binary.LittleEndian.Uint32(data))))
	case FormatFloat64:
		return floatToInt16(math.Float64frombits(binary.LittleEndian.Uint64(data)))
	default:
		panic("unsupported PCM format")
	}
}

func (c *PCMConverter) convertFrames(data []byte) []int16 {
	inWidth := c.in.bytesPerSample()
	frames := len(data) / c.in.frameSize()
	converted := make([]int16, frames*c.out.Channels)
	for frame := 0; frame < frames; frame++ {
		frameData := data[frame*c.in.frameSize():]
		for outChannel := 0; outChannel < c.out.Channels; outChannel++ {
			var sample int16
			switch {
			case c.in.Channels == c.out.Channels:
				sample = decodeInt16Sample(frameData[outChannel*inWidth:], c.in.Format)
			case c.out.Channels == 1:
				sum := int64(0)
				for inChannel := 0; inChannel < c.in.Channels; inChannel++ {
					sum += int64(decodeInt16Sample(frameData[inChannel*inWidth:], c.in.Format))
				}
				sample = int16(sum / int64(c.in.Channels))
			case c.in.Channels == 1:
				sample = decodeInt16Sample(frameData, c.in.Format)
			default:
				// For uncommon multi-channel upmixes, repeat the source layout.
				inChannel := outChannel % c.in.Channels
				sample = decodeInt16Sample(frameData[inChannel*inWidth:], c.in.Format)
			}
			converted[frame*c.out.Channels+outChannel] = sample
		}
	}
	return converted
}

func int16Bytes(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}
	return out
}

// appendResampled appends one input frame. phase is measured in output-rate
// units between previous and current; integer arithmetic avoids long-run drift.
func (c *PCMConverter) appendResampled(current []int16, out []int16) []int16 {
	if c.previous == nil {
		c.previous = append(c.previous, current...)
		return out
	}

	inRate := int64(c.in.SampleRate)
	outRate := int64(c.out.SampleRate)
	for c.phase < outRate {
		for channel := range current {
			before := int64(c.previous[channel])
			delta := int64(current[channel]) - before
			sample := before + delta*c.phase/outRate
			out = append(out, int16(sample))
		}
		c.phase += inRate
	}
	c.phase -= outRate
	copy(c.previous, current)
	return out
}

// Convert accepts arbitrary byte chunks and retains incomplete frames for the
// next call. Returned memory is independent of both input and ffaudio buffers.
func (c *PCMConverter) Convert(p []byte) ([]byte, error) {
	if len(p) == 0 {
		return nil, nil
	}
	data := p
	if len(c.pending) != 0 {
		data = make([]byte, 0, len(c.pending)+len(p))
		data = append(data, c.pending...)
		data = append(data, p...)
		c.pending = c.pending[:0]
	}

	frameSize := c.in.frameSize()
	complete := len(data) / frameSize * frameSize
	if remainder := len(data) - complete; remainder != 0 {
		c.pending = append(c.pending[:0], data[complete:]...)
	}
	if complete == 0 {
		return nil, nil
	}

	converted := c.convertFrames(data[:complete])
	if c.in.SampleRate == c.out.SampleRate {
		return int16Bytes(converted), nil
	}

	channels := c.out.Channels
	resampled := make([]int16, 0, len(converted))
	for offset := 0; offset < len(converted); offset += channels {
		resampled = c.appendResampled(converted[offset:offset+channels], resampled)
	}
	return int16Bytes(resampled), nil
}

// Flush emits the final frame only when an output sample falls exactly on the
// stream endpoint. It does not pad the stream with extra interpolated audio.
func (c *PCMConverter) Flush() []byte {
	if c.in.SampleRate == c.out.SampleRate || c.previous == nil || c.phase != 0 {
		return nil
	}
	c.phase = int64(c.out.SampleRate)
	return int16Bytes(c.previous)
}
