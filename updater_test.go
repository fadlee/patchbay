package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1, v2 string
		want   int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"v1.2.0", "1.1.9", 1},
		{"1.10.0", "1.2.0", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.0.0-rc1", "1.0.0", 0},
	}

	for _, tt := range tests {
		got := compareVersions(tt.v1, tt.v2)
		if got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.v1, tt.v2, got, tt.want)
		}
	}
}

func TestMatchAsset(t *testing.T) {
	assets := []ghAsset{
		{Name: "patchbay-setup-amd64.exe", BrowserDownloadURL: "https://example.com/setup.exe"},
		{Name: "patchbay-windows-amd64.exe", BrowserDownloadURL: "https://example.com/win.exe"},
	}

	winMatch := matchAsset(assets, "windows", "amd64")
	if winMatch == nil || winMatch.Name != "patchbay-setup-amd64.exe" {
		t.Errorf("expected setup.exe match for windows, got %v", winMatch)
	}

	linuxMatch := matchAsset(assets, "linux", "amd64")
	if linuxMatch != nil {
		t.Errorf("expected nil asset match for linux, got %v", linuxMatch)
	}
}
func TestUpdaterCheckAndDownload(t *testing.T) {
	var downloadHit bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download/setup.exe" {
			downloadHit = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("fake installer binary content"))
			return
		}

		rel := ghRelease{
			TagName: "v1.5.0",
			HTMLURL: "https://github.com/fadlee/patchbay/releases/tag/v1.5.0",
			Body:    "Bug fixes & improvements",
			Assets: []ghAsset{
				{
					Name:               "patchbay-setup-amd64.exe",
					Size:               27,
					BrowserDownloadURL: "/download/setup.exe",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer ts.Close()

	u := &Updater{
		repo:    "fadlee/patchbay",
		baseURL: ts.URL,
		client:  ts.Client(),
	}

	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}

	if !info.UpdateAvail {
		t.Errorf("expected update available, got false")
	}
	if info.LatestVersion != "1.5.0" {
		t.Errorf("expected latest version 1.5.0, got %s", info.LatestVersion)
	}

	// Test download
	var dlCount, dlTotal int64
	tmpFile, err := u.DownloadAsset(context.Background(), ts.URL+"/download/setup.exe", func(d, total int64) {
		dlCount = d
		dlTotal = total
	})
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if !downloadHit {
		t.Errorf("expected download endpoint hit")
	}
	if dlCount == 0 {
		t.Errorf("expected progress callback called")
	}
	_ = dlTotal
	_ = tmpFile
}
