package tunnel

import (
	"testing"
)

func TestParseCloudflaredURL(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantURL string
		wantOK  bool
	}{
		{
			name:    "typical output line",
			line:    "2026-03-24T12:00:00Z INF |  https://random-slug.trycloudflare.com",
			wantURL: "https://random-slug.trycloudflare.com",
			wantOK:  true,
		},
		{
			name:    "connector URL line",
			line:    "2026-03-24T12:00:00Z INF +---------------------------------------------------+",
			wantURL: "",
			wantOK:  false,
		},
		{
			name:    "no URL",
			line:    "some random log line without a URL",
			wantURL: "",
			wantOK:  false,
		},
		{
			name:    "empty line",
			line:    "",
			wantURL: "",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, ok := ParseCloudflaredURL(tt.line)
			if ok != tt.wantOK {
				t.Errorf("ok=%v, want %v", ok, tt.wantOK)
			}
			if url != tt.wantURL {
				t.Errorf("url=%q, want %q", url, tt.wantURL)
			}
		})
	}
}
