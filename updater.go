package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	// Version is the current version of the application.
	// Can be overridden at build time via -ldflags "-X main.Version=x.y.z".
	Version = "1.0.0"

	// GitHubRepo is the default repository checked for updates.
	GitHubRepo = "fadlee/patchbay"
)

// UpdateInfo holds details about an available update.
type UpdateInfo struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	UpdateAvail    bool   `json:"update_available"`
	ReleaseNotes   string `json:"release_notes"`
	ReleaseURL     string `json:"release_url"`
	AssetURL       string `json:"asset_url,omitempty"`
	AssetName      string `json:"asset_name,omitempty"`
	AssetSize      int64  `json:"asset_size,omitempty"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	HTMLURL string    `json:"html_url"`
	Body    string    `json:"body"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Updater handles checking and applying software updates.
type Updater struct {
	repo       string
	baseURL    string
	client     *http.Client
	httpClient func() *http.Client
}
func NewUpdater(repo string) *Updater {
	if repo == "" {
		repo = GitHubRepo
	}
	return &Updater{
		repo: repo,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Check queries GitHub Releases API to see if a newer version is available.
func (u *Updater) Check(ctx context.Context) (*UpdateInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", u.repo)
	if u.baseURL != "" {
		url = u.baseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Patchbay-Updater/"+Version)

	cli := u.client
	if u.httpClient != nil {
		cli = u.httpClient()
	}

	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release json: %w", err)
	}

	latestVer := strings.TrimPrefix(rel.TagName, "v")
	avail := compareVersions(latestVer, Version) > 0

	info := &UpdateInfo{
		CurrentVersion: Version,
		LatestVersion:  latestVer,
		UpdateAvail:    avail,
		ReleaseNotes:   rel.Body,
		ReleaseURL:     rel.HTMLURL,
	}

	// Find best matching asset for OS/arch
	asset := matchAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if asset != nil {
		info.AssetURL = asset.BrowserDownloadURL
		info.AssetName = asset.Name
		info.AssetSize = asset.Size
	}

	return info, nil
}

// DownloadAsset downloads the update file from assetURL into a temp file and returns its path.
func (u *Updater) DownloadAsset(ctx context.Context, assetURL string, progressFn func(downloaded, total int64)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", "Patchbay-Updater/"+Version)

	cli := u.client
	if u.httpClient != nil {
		cli = u.httpClient()
	}

	resp, err := cli.Do(req)
	if err != nil {
		return "", fmt.Errorf("download asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	filename := filepath.Base(resp.Request.URL.Path)
	if filename == "" || filename == "." || filename == "/" {
		filename = "patchbay-update.exe"
	}

	tmpFile, err := os.CreateTemp("", "patchbay-update-*"+filepath.Ext(filename))
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	var writer io.Writer = tmpFile
	if progressFn != nil {
		writer = &progressWriter{
			dst:        tmpFile,
			total:      resp.ContentLength,
			progressFn: progressFn,
		}
	}

	if _, err := io.Copy(writer, resp.Body); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("write update file: %w", err)
	}

	return tmpFile.Name(), nil
}

// LaunchInstaller launches the installer or updater executable.
func LaunchInstaller(installerPath string) error {
	if runtime.GOOS == "windows" {
		// Launch installer detached
		cmd := exec.Command(installerPath)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("launch installer: %w", err)
		}
		return nil
	}
	return fmt.Errorf("auto-installer not supported on %s", runtime.GOOS)
}

type progressWriter struct {
	dst        io.Writer
	downloaded int64
	total      int64
	progressFn func(downloaded, total int64)
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.dst.Write(p)
	if n > 0 {
		pw.downloaded += int64(n)
		if pw.progressFn != nil {
			pw.progressFn(pw.downloaded, pw.total)
		}
	}
	return n, err
}

func matchAsset(assets []ghAsset, targetOS, targetArch string) *ghAsset {
	if len(assets) == 0 {
		return nil
	}

	// For Windows, prefer installer setup .exe, then windows-amd64 .exe
	if targetOS == "windows" {
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if strings.Contains(name, "setup") && strings.HasSuffix(name, ".exe") {
				return &assets[i]
			}
		}
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if strings.Contains(name, "windows") && strings.HasSuffix(name, ".exe") {
				return &assets[i]
			}
		}
		for i := range assets {
			if strings.HasSuffix(strings.ToLower(assets[i].Name), ".exe") {
				return &assets[i]
			}
		}
	}

	// Generic match
	for i := range assets {
		name := strings.ToLower(assets[i].Name)
		if strings.Contains(name, targetOS) && (targetArch == "" || strings.Contains(name, targetArch)) {
			return &assets[i]
		}
	}

	return &assets[0]
}

// compareVersions returns 1 if v1 > v2, -1 if v1 < v2, 0 if v1 == v2.
func compareVersions(v1, v2 string) int {
	clean1 := strings.TrimPrefix(strings.TrimSpace(v1), "v")
	clean2 := strings.TrimPrefix(strings.TrimSpace(v2), "v")

	parts1 := strings.Split(clean1, ".")
	parts2 := strings.Split(clean2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := range maxLen {
		var n1, n2 int
		if i < len(parts1) {
			// Extract leading integer
			numStr := leadingDigits(parts1[i])
			n1, _ = strconv.Atoi(numStr)
		}
		if i < len(parts2) {
			numStr := leadingDigits(parts2[i])
			n2, _ = strconv.Atoi(numStr)
		}
		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}
	return 0
}

func leadingDigits(s string) string {
	var b strings.Builder
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
		} else {
			break
		}
	}
	return b.String()
}
