package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var trycloudflareRe = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)

func ParseCloudflaredURL(line string) (string, bool) {
	match := trycloudflareRe.FindString(line)
	if match == "" {
		return "", false
	}
	return match, true
}

type Tunnel struct {
	cmd *exec.Cmd
	URL string
}

func Start(ctx context.Context, localPort int) (*Tunnel, error) {
	cloudflaredPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return nil, fmt.Errorf("cloudflared not found on PATH. Install from: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/")
	}

	localURL := fmt.Sprintf("http://localhost:%d", localPort)
	cmd := exec.CommandContext(ctx, cloudflaredPath, "tunnel", "--url", localURL)
	cmd.Stdout = os.Stdout

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting cloudflared: %w", err)
	}

	t := &Tunnel{cmd: cmd}

	urlCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("[cloudflared] %s", line)
			if url, ok := ParseCloudflaredURL(line); ok {
				urlCh <- url
			}
		}
	}()

	select {
	case url := <-urlCh:
		t.URL = url
		return t, nil
	case <-ctx.Done():
		cmd.Process.Kill()
		return nil, ctx.Err()
	}
}

func (t *Tunnel) StreamURL() string {
	return strings.TrimRight(t.URL, "/") + "/stream.m3u8"
}

func (t *Tunnel) Stop() {
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
}
