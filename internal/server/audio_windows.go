//go:build windows

package server

import (
	"context"
	"io"

	"github.com/vexedaa/vrshare/internal/audio"
)

type audioCapturer struct {
	c  *audio.Capturer
	aw *audio.AsyncWriter
}

func newAudioCapturer(ctx context.Context, w io.WriteCloser) *audioCapturer {
	aw := audio.NewAsyncWriter(ctx, w, 256) // ~2.5s buffer at 48kHz stereo 16-bit
	return &audioCapturer{c: audio.NewCapturer(aw), aw: aw}
}

func (a *audioCapturer) start(ctx context.Context) {
	a.c.Start(ctx)
}

// signalReady tells the audio drain goroutine that FFmpeg has started encoding
// its first frame. Any audio buffered before this call is discarded so that
// audio PTS=0 aligns with video PTS=0, eliminating the startup A/V sync offset.
func (a *audioCapturer) signalReady() {
	a.aw.SignalReady()
}
