//go:build windows

package server

import (
	"context"
	"io"

	"github.com/vexedaa/vrshare/internal/audio"
)

type audioCapturer struct {
	c *audio.Capturer
}

func newAudioCapturer(ctx context.Context, w io.WriteCloser) *audioCapturer {
	aw := audio.NewAsyncWriter(ctx, w, 256) // ~2.5s buffer at 48kHz stereo 16-bit
	return &audioCapturer{c: audio.NewCapturer(aw)}
}

func (a *audioCapturer) start(ctx context.Context) {
	a.c.Start(ctx)
}
