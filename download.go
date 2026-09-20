package main

// Firmware resolution: embedded (bundled) → local cache → download from the
// falconshop GitHub release. Only the top-selling models are bundled into the
// exe; the rest download on demand and are cached for next time.

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const releaseBase = "https://github.com/falconshop/elrs-auto-firmware/releases/download/3.6.4"

var errNoInternet = fmt.Errorf("no-internet")

func cacheDir() string {
	d := filepath.Join(os.TempDir(), "elrs-updater-cache")
	os.MkdirAll(d, 0o755)
	return d
}

// binAvailableLocally reports whether a firmware file can be flashed with no
// internet (it is embedded in the exe or already downloaded to the cache).
func binAvailableLocally(file string) bool {
	if f, err := fwFS.Open("firmware/" + file); err == nil {
		f.Close()
		return true
	}
	if st, err := os.Stat(filepath.Join(cacheDir(), file)); err == nil && st.Size() > 1000 {
		return true
	}
	return false
}

// getBin returns firmware bytes, downloading and caching from the release if the
// file is neither embedded nor cached. Returns errNoInternet when a download is
// required but the network is unreachable.
func getBin(file string) ([]byte, error) {
	if data, err := fs.ReadFile(fwFS, "firmware/"+file); err == nil && len(data) > 0 {
		return data, nil
	}
	cp := filepath.Join(cacheDir(), file)
	if data, err := os.ReadFile(cp); err == nil && len(data) > 1000 {
		return data, nil
	}
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(releaseBase + "/" + file)
	if err != nil {
		return nil, errNoInternet
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("다운로드 실패 (%d): %s", resp.StatusCode, file)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errNoInternet
	}
	if len(data) < 1000 {
		return nil, fmt.Errorf("다운로드된 파일이 손상됨: %s", file)
	}
	_ = os.WriteFile(cp, data, 0o644)
	return data, nil
}

// hasInternet does a quick reachability check to the release host.
func hasInternet() bool {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Head("https://github.com")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}
