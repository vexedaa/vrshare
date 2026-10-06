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

func newAudioCapturer(ctx context.Context, w io.WriteCloser, device string) *audioCapturer {
	aw := audio.NewAsyncWriter(ctx, w, 256) // ~2.5s buffer at 48kHz stereo 16-bit
	c := audio.NewCapturer(aw)
	c.SetDevice(device)
	return &audioCapturer{c: c, aw: aw}
}

// setDevice switches the captured source; takes effect immediately.
func (a *audioCapturer) setDevice(device string) {
	a.c.SetDevice(device)
}

func (a *audioCapturer) start(ctx context.Context) {
	a.c.Start(ctx)
}

// discardStale drops audio buffered while no FFmpeg was reading the pipe and
// returns how many ~10ms chunks were dropped.
func (a *audioCapturer) discardStale() int {
	return a.aw.Discard()
}
