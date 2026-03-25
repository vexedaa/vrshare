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
	"syscall"
)

var trycloudflareRe = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)

// ParseCloudflaredURL extracts the tunnel URL from a cloudflared log line.
func ParseCloudflaredURL(line string) (string, bool) {
	match := trycloudflareRe.FindString(line)
	if match == "" {
		return "", false
	}
	return match, true
}

// Tunnel represents an active tunnel process.
type Tunnel struct {
	cmd *exec.Cmd
	URL string
}

// StartCloudflare launches a cloudflared quick tunnel.
func StartCloudflare(ctx context.Context, localPort int) (*Tunnel, error) {
	cloudflaredPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return nil, fmt.Errorf("cloudflared not found on PATH. Install from: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/")
	}

	localURL := fmt.Sprintf("http://localhost:%d", localPort)
	cmd := exec.CommandContext(ctx, cloudflaredPath, "tunnel", "--url", localURL)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
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
	errCh := make(chan error, 1)
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
	go func() {
		errCh <- cmd.Wait()
	}()

	select {
	case url := <-urlCh:
		t.URL = url
		return t, nil
	case err := <-errCh:
		if err != nil {
			return nil, fmt.Errorf("cloudflared exited: %w", err)
		}
		return nil, fmt.Errorf("cloudflared exited without providing URL")
	case <-ctx.Done():
		cmd.Process.Kill()
		return nil, ctx.Err()
	}
}

// StartTailscale launches a Tailscale Funnel on the given port.
// Returns a stable HTTPS URL with no request timeout limits.
func StartTailscale(ctx context.Context, localPort int) (*Tunnel, error) {
	tailscalePath, err := exec.LookPath("tailscale")
	if err != nil {
		return nil, fmt.Errorf("tailscale not found on PATH. Install from: https://tailscale.com/download")
	}

	cmd := exec.CommandContext(ctx, tailscalePath, "funnel", fmt.Sprintf("%d", localPort))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting tailscale funnel: %w", err)
	}

	t := &Tunnel{cmd: cmd}

	// tailscale funnel outputs the URL to stdout like:
	// https://machine-name.tailnet.ts.net/
	urlCh := make(chan string, 1)
	errCh := make(chan error, 1)
	tsURLRe := regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.ts\.net`)

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("[tailscale] %s", line)
			if match := tsURLRe.FindString(line); match != "" {
				urlCh <- match
			}
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("[tailscale] %s", scanner.Text())
		}
	}()
	go func() {
		errCh <- cmd.Wait()
	}()

	select {
	case url := <-urlCh:
		t.URL = url
		return t, nil
	case err := <-errCh:
		if err != nil {
			return nil, fmt.Errorf("tailscale exited: %w", err)
		}
		return nil, fmt.Errorf("tailscale exited without providing URL")
	case <-ctx.Done():
		cmd.Process.Kill()
		return nil, ctx.Err()
	}
}

// Start launches a tunnel using the specified provider.
// Supported providers: "cloudflare", "tailscale"
func Start(ctx context.Context, provider string, localPort int) (*Tunnel, error) {
	switch provider {
	case "tailscale":
		return StartTailscale(ctx, localPort)
	case "cloudflare":
		return StartCloudflare(ctx, localPort)
	default:
		return nil, fmt.Errorf("unknown tunnel provider: %q (use cloudflare or tailscale)", provider)
	}
}

func (t *Tunnel) StreamURL() string {
	return strings.TrimRight(t.URL, "/") + "/stream.m3u8"
}

func (t *Tunnel) MP4URL() string {
	return strings.TrimRight(t.URL, "/") + "/stream.mp4"
}

func (t *Tunnel) Stop() {
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
}
