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

func newAudioCapturer(w io.Writer) *audioCapturer {
	return &audioCapturer{c: audio.NewCapturer(w)}
}

func (a *audioCapturer) start(ctx context.Context) {
	a.c.Start(ctx)
}
