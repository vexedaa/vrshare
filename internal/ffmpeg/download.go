package ffmpeg

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	windowsFFmpegURL = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
	linuxFFmpegURL   = "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz"
)

func PromptAndDownload() (string, error) {
	fmt.Println("FFmpeg was not found on your system.")
	fmt.Print("Would you like to download it automatically? [Y/n] ")

	var response string
	fmt.Scanln(&response)
	response = strings.TrimSpace(strings.ToLower(response))

	if response != "" && response != "y" && response != "yes" {
		return "", fmt.Errorf("FFmpeg is required. Install it manually and ensure it's on your PATH.\n" +
			"Download from: https://ffmpeg.org/download.html")
	}

	cacheDir := defaultCacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		return downloadWindows(cacheDir)
	default:
		return "", fmt.Errorf("automatic download not yet supported on %s.\n"+
			"Install FFmpeg manually: https://ffmpeg.org/download.html", runtime.GOOS)
	}
}

func downloadWindows(cacheDir string) (string, error) {
	log.Println("Downloading FFmpeg...")

	zipPath := filepath.Join(cacheDir, "ffmpeg.zip")
	if err := downloadFile(zipPath, windowsFFmpegURL); err != nil {
		return "", fmt.Errorf("downloading FFmpeg: %w", err)
	}
	defer os.Remove(zipPath)

	log.Println("Extracting FFmpeg...")
	ffmpegPath, err := extractFFmpegFromZip(zipPath, cacheDir)
	if err != nil {
		return "", fmt.Errorf("extracting FFmpeg: %w", err)
	}

	log.Printf("FFmpeg installed to %s", ffmpegPath)
	return ffmpegPath, nil
}

func downloadFile(dest, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func extractFFmpegFromZip(zipPath, destDir string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer r.Close()

	for _, f := range r.File {
		if strings.HasSuffix(f.Name, "bin/ffmpeg.exe") || strings.HasSuffix(f.Name, "bin\\ffmpeg.exe") {
			destPath := filepath.Join(destDir, "ffmpeg.exe")
			src, err := f.Open()
			if err != nil {
				return "", err
			}
			defer src.Close()

			dst, err := os.Create(destPath)
			if err != nil {
				return "", err
			}
			defer dst.Close()

			if _, err := io.Copy(dst, src); err != nil {
				return "", err
			}
			return destPath, nil
		}
	}

	return "", fmt.Errorf("ffmpeg.exe not found in archive")
}
