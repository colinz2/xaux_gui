package vad

const FrameSamples = 256

type Detector interface {
	Process([]int16) (bool, error)
	Close() error
}
