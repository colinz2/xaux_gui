package ffaudio

import (
	"encoding/binary"
	"math"
	"reflect"
	"testing"
)

func decodeTestInt16(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return samples
}

func TestPCMConverterSampleFormats(t *testing.T) {
	float32Bytes := func(v float32) []byte {
		p := make([]byte, 4)
		binary.LittleEndian.PutUint32(p, math.Float32bits(v))
		return p
	}
	float64Bytes := func(v float64) []byte {
		p := make([]byte, 8)
		binary.LittleEndian.PutUint64(p, math.Float64bits(v))
		return p
	}
	tests := []struct {
		name   string
		format int
		input  []byte
		want   int16
	}{
		{name: "int8", format: FormatInt8, input: []byte{0x80}, want: -32768},
		{name: "uint8", format: FormatUint8, input: []byte{0}, want: -32768},
		{name: "int16", format: FormatInt16, input: []byte{0x34, 0x12}, want: 0x1234},
		{name: "int24-max", format: FormatInt24, input: []byte{0xff, 0xff, 0x7f}, want: 32767},
		{name: "int24-min", format: FormatInt24, input: []byte{0x00, 0x00, 0x80}, want: -32768},
		{name: "int24-in-32-max", format: FormatInt24_4, input: []byte{0xff, 0xff, 0xff, 0x7f}, want: 32767},
		{name: "int24-in-32-min", format: FormatInt24_4, input: []byte{0x00, 0x00, 0x00, 0x80}, want: -32768},
		{name: "int32", format: FormatInt32, input: []byte{0x00, 0x00, 0x00, 0x80}, want: -32768},
		{name: "float32", format: FormatFloat32, input: float32Bytes(-0.5), want: -16384},
		{name: "float64", format: FormatFloat64, input: float64Bytes(1), want: 32767},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converter, err := NewPCMConverter(
				PCMFormat{SampleRate: 16000, Channels: 1, Format: tt.format},
				PCMFormat{SampleRate: 16000, Channels: 1, Format: FormatInt16},
			)
			if err != nil {
				t.Fatal(err)
			}
			got, err := converter.Convert(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if samples := decodeTestInt16(got); !reflect.DeepEqual(samples, []int16{tt.want}) {
				t.Fatalf("got %v, want %d", samples, tt.want)
			}
		})
	}
}

func TestPCMConverterPartialFramesAndDownmix(t *testing.T) {
	converter, err := NewPCMConverter(
		PCMFormat{SampleRate: 48000, Channels: 2, Format: FormatInt16},
		PCMFormat{SampleRate: 48000, Channels: 1, Format: FormatInt16},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Stereo frames: (1000, 3000) and (-1000, 1000).
	input := []byte{0xe8, 0x03, 0xb8, 0x0b, 0x18, 0xfc, 0xe8, 0x03}
	var output []byte
	for _, size := range []int{1, 2, 5} {
		chunk := input[:size]
		input = input[size:]
		converted, err := converter.Convert(chunk)
		if err != nil {
			t.Fatal(err)
		}
		output = append(output, converted...)
	}
	if got, want := decodeTestInt16(output), []int16{2000, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPCMConverterDownsampleAndFlush(t *testing.T) {
	converter, err := NewPCMConverter(
		PCMFormat{SampleRate: 48000, Channels: 2, Format: FormatInt16},
		PCMFormat{SampleRate: 16000, Channels: 1, Format: FormatInt16},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := make([]byte, 0, 4*2*2)
	for _, sample := range []int16{1000, 2000, 3000, 4000} {
		for channel := 0; channel < 2; channel++ {
			var p [2]byte
			binary.LittleEndian.PutUint16(p[:], uint16(sample))
			input = append(input, p[:]...)
		}
	}
	converted, err := converter.Convert(input)
	if err != nil {
		t.Fatal(err)
	}
	converted = append(converted, converter.Flush()...)
	if got, want := decodeTestInt16(converted), []int16{1000, 4000}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPCMConverterUpsample(t *testing.T) {
	converter, err := NewPCMConverter(
		PCMFormat{SampleRate: 16000, Channels: 1, Format: FormatInt16},
		PCMFormat{SampleRate: 48000, Channels: 1, Format: FormatInt16},
	)
	if err != nil {
		t.Fatal(err)
	}
	converted, err := converter.Convert([]byte{0, 0, 0xe8, 0x03})
	if err != nil {
		t.Fatal(err)
	}
	converted = append(converted, converter.Flush()...)
	if got, want := decodeTestInt16(converted), []int16{0, 333, 666, 1000}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPCMConverterRejectsInvalidFormats(t *testing.T) {
	if _, err := NewPCMConverter(PCMFormat{}, PCMFormat{}); err == nil {
		t.Fatal("expected invalid input format error")
	}
	if _, err := NewPCMConverter(
		PCMFormat{SampleRate: 16000, Channels: 1, Format: FormatFloat32},
		PCMFormat{SampleRate: 16000, Channels: 1, Format: FormatFloat32},
	); err == nil {
		t.Fatal("expected non-int16 output format error")
	}
}
