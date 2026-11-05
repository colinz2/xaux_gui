//go:build !tenvad || !windows || !cgo

package vad

import "fmt"

func New() (Detector, error) {
	return nil, fmt.Errorf("TEN VAD requires Windows, cgo and the tenvad build tag")
}
