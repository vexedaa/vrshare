package gui

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func checkTunnelProviders() []TunnelProviderStatus {
	return []TunnelProviderStatus{
		checkCloudflare(),
		checkTailscale(),
	}
}

func checkCloudflare() TunnelProviderStatus {
	s := TunnelProviderStatus{
		Name:  "cloudflare",
		Label: "Cloudflare Tunnel",
	}

	_, err := exec.LookPath("cloudflared")
	if err != nil {
		s.StatusText = "Not installed"
		return s
	}
	s.Installed = true
	// Quick tunnels don't require auth
	s.Authorized = true
	s.StatusText = "Ready (no login required)"
	return s
}

func checkTailscale() TunnelProviderStatus {
	s := TunnelProviderStatus{
		Name:  "tailscale",
		Label: "Tailscale Funnel",
	}

	_, err := exec.LookPath("tailscale")
	if err != nil {
		s.StatusText = "Not installed"
		return s
	}
	s.Installed = true

	// Check if logged in via `tailscale status`
	// Use HideWindow only (not CREATE_NO_WINDOW) to keep pipes working
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tailscale", "status")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if err != nil {
		// tailscale status exits non-zero when not logged in
		if strings.Contains(output, "NeedsLogin") || strings.Contains(output, "not logged in") {
			s.StatusText = "Not logged in"
		} else if strings.Contains(output, "stopped") {
			s.StatusText = "Tailscale stopped"
		} else {
			s.StatusText = "Not connected"
		}
		return s
	}

	// If it succeeds, we're logged in and connected
	s.Authorized = true
	s.StatusText = "Ready"
	return s
}

func authorizeTunnel(provider string) (string, error) {
	switch provider {
	case "tailscale":
		return authorizeTailscale()
	case "cloudflare":
		return "Cloudflare quick tunnels don't require login.", nil
	default:
		return "", fmt.Errorf("unknown provider: %s", provider)
	}
}

func authorizeTailscale() (string, error) {
	_, err := exec.LookPath("tailscale")
	if err != nil {
		return "", fmt.Errorf("tailscale not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tailscale", "login")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("creating pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("creating pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting tailscale login: %w", err)
	}

	// Scan both stdout and stderr for a login URL
	urlCh := make(chan string, 1)
	scan := func(s *bufio.Scanner) {
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if strings.Contains(line, "https://") {
				// Extract URL from line
				for _, word := range strings.Fields(line) {
					if strings.HasPrefix(word, "https://") {
						select {
						case urlCh <- word:
						default:
						}
						return
					}
				}
			}
			if strings.Contains(line, "already logged in") || strings.Contains(line, "Success") {
				select {
				case urlCh <- "already_logged_in":
				default:
				}
				return
			}
		}
	}
	go scan(bufio.NewScanner(stdout))
	go scan(bufio.NewScanner(stderr))

	select {
	case url := <-urlCh:
		if url == "already_logged_in" {
			cmd.Process.Kill()
			return "Already logged in to Tailscale.", nil
		}
		openBrowser(url)
		// Don't wait for the process — it stays alive until auth completes
		go cmd.Wait()
		return fmt.Sprintf("Opening browser for Tailscale login."), nil
	case <-ctx.Done():
		cmd.Process.Kill()
		return "", fmt.Errorf("tailscale login timed out")
	}
}

func openBrowser(url string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Start()
}
