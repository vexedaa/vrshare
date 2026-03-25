//go:build !windows

package server

import (
	"context"
	"io"
	"log"
)

type audioCapturer struct{}

func newAudioCapturer(_ io.Writer) *audioCapturer {
	return &audioCapturer{}
}

func (a *audioCapturer) start(_ context.Context) {
	log.Println("Audio capture not supported on this platform")
}
