package main

// ELRS receiver firmware updater — customer-facing tool.
//
// Two modes from one binary:
//   * GUI (default, double-click): starts a tiny local web server, opens it as an
//     Edge "app" window, auto-detects the connected receiver, and flashes the
//     matching embedded firmware over WiFi OTA.
//   * CLI (dev/test): `elrs-updater <firmware.bin> [--force]` flashes an external
//     .bin — handy for bench-testing against a real receiver.
//
// Stdlib only; firmware images + web UI are embedded so it ships as one .exe.

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

//go:embed firmware
var fwFS embed.FS

// host is the receiver address; overridable via ELRS_HOST for testing.
var host = envOr("ELRS_HOST", defaultHost)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type fwVariant struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	File  string `json:"file"`
}

type fwModel struct {
	Match    string      `json:"match"`
	Name     string      `json:"name"`
	Variants []fwVariant `json:"variants"`
}

type fwManifest struct {
	Models []fwModel `json:"models"`
}

func loadManifest() fwManifest {
	var m fwManifest
	b, err := fs.ReadFile(fwFS, "firmware/manifest.json")
	if err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// matchModel returns the first model whose Match is a case-insensitive
// substring of the receiver's product name.
func matchModel(m fwManifest, productName string) *fwModel {
	p := strings.ToLower(productName)
	for i := range m.Models {
		if m.Models[i].Match != "" && strings.Contains(p, strings.ToLower(m.Models[i].Match)) {
			return &m.Models[i]
		}
	}
	return nil
}

// fwExists reports whether a firmware file is actually embedded.
func fwExists(file string) bool {
	if file == "" {
		return false
	}
	f, err := fwFS.Open("firmware/" + file)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// availableVariants returns the model's variants. All are obtainable — embedded,
// cached, or downloaded from the release — so all are offered.
func availableVariants(model *fwModel) []fwVariant {
	if model == nil {
		return nil
	}
	return model.Variants
}

func main() {
	// CLI mode if a positional (non-flag) argument is present.
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "-") {
			runCLI()
			return
		}
	}
	runGUI()
}

// --------------------------------------------------------------------- GUI mode

var (
	lastPing = time.Now()
	pingMu   sync.Mutex
)

func runGUI() {
	manifest := loadManifest()

	ln, err := net.Listen("tcp", envOr("ELRS_ADDR", "127.0.0.1:0"))
	if err != nil {
		return
	}
	url := fmt.Sprintf("http://%s/", ln.Addr().String())

	mux := http.NewServeMux()

	// Serve the embedded web UI at "/".
	uiSub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(uiSub)))

	mux.HandleFunc("/api/detect", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		t, err := getTarget(host, 3*time.Second)
		if err != nil || t == nil {
			json.NewEncoder(w).Encode(map[string]any{"found": false})
			return
		}
		model := matchModel(manifest, t.ProductName)
		variants := availableVariants(model)
		pub := make([]map[string]string, 0, len(variants))
		for _, v := range variants {
			pub = append(pub, map[string]string{"id": v.ID, "label": v.Label})
		}
		json.NewEncoder(w).Encode(map[string]any{
			"found":        true,
			"product_name": t.ProductName,
			"version":      t.Version,
			"module_type":  t.ModuleType,
			"matched":      len(variants) > 0,
			"variants":     pub,
		})
	})

	mux.HandleFunc("/api/flash", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		send := func(event, data string) {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
		}
		done := func(status, msg, label string) {
			b, _ := json.Marshal(map[string]string{"status": status, "msg": msg, "label": label})
			send("done", string(b))
		}

		t, err := getTarget(host, 3*time.Second)
		if err != nil || t == nil {
			done("error", "수신기 연결이 끊겼습니다. WiFi 연결을 확인하세요.", "")
			return
		}
		model := matchModel(manifest, t.ProductName)
		variants := availableVariants(model)
		if len(variants) == 0 {
			done("error", "이 수신기에 맞는 펌웨어가 이 도구에 없습니다.", "")
			return
		}
		// Use the requested variant, else the first available one.
		chosen := variants[0]
		if id := r.URL.Query().Get("variant"); id != "" {
			for _, v := range variants {
				if v.ID == id {
					chosen = v
					break
				}
			}
		}
		data, err := getBin(chosen.File)
		if err != nil {
			if err == errNoInternet {
				done("error", "이 모델 펌웨어를 받으려면 인터넷이 필요합니다.\n인터넷 WiFi로 바꿔 잠시 연결(자동 다운로드)한 뒤, 다시 'ExpressLRS RX'에 연결하면 이어집니다.", "")
			} else {
				done("error", err.Error(), "")
			}
			return
		}

		// Optional binding phrase — set on the current firmware before flashing; the
		// UID lives in config and survives the update (our bins bake no phrase).
		if phrase := strings.TrimSpace(r.URL.Query().Get("phrase")); phrase != "" {
			if err := setBindingPhrase(host, phrase, 15*time.Second); err != nil {
				done("error", "바인딩 문구 설정 실패: "+err.Error(), "")
				return
			}
		}

		lastPct := -1
		res, err := uploadFirmware(data, host, false, func(read, total int64) {
			pct := int(read * 100 / total)
			if pct != lastPct {
				lastPct = pct
				send("progress", fmt.Sprintf("%d", pct))
			}
		}, 120*time.Second)
		if err != nil {
			done("error", err.Error(), "")
			return
		}
		// gz => ESP8285 (eboot decompresses on reboot, ~10s); raw .bin => ESP32/C3 (fast).
		b2, _ := json.Marshal(map[string]any{
			"status": res.Status, "msg": cleanMsg(res.Msg),
			"label": chosen.Label, "gz": strings.HasSuffix(chosen.File, ".gz"),
		})
		send("done", string(b2))
	})

	// Full model list for the manual "수신기 선택" dropdown.
	mux.HandleFunc("/api/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		out := make([]map[string]string, 0, len(manifest.Models))
		for _, m := range manifest.Models {
			out = append(out, map[string]string{"name": m.Name, "match": m.Match})
		}
		json.NewEncoder(w).Encode(out)
	})

	// Pre-download a chosen model's firmware (both variants) while internet is
	// available, so it can later be flashed while connected to the RX WiFi.
	mux.HandleFunc("/api/prepare", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		match := r.URL.Query().Get("model")
		var model *fwModel
		for i := range manifest.Models {
			if manifest.Models[i].Match == match {
				model = &manifest.Models[i]
				break
			}
		}
		if model == nil {
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "unknown"})
			return
		}
		for _, v := range model.Variants {
			if !binAvailableLocally(v.File) {
				if _, err := getBin(v.File); err != nil {
					json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "no-internet"})
					return
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		pingMu.Lock()
		lastPing = time.Now()
		pingMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		go func() { time.Sleep(200 * time.Millisecond); os.Exit(0) }()
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)

	// Exit when the UI window is gone (no heartbeat for a while).
	// Skipped in headless test mode (ELRS_NOBROWSER) so the server stays up.
	if os.Getenv("ELRS_NOBROWSER") == "" {
		go func() {
			time.Sleep(6 * time.Second) // grace for the window to open
			for {
				time.Sleep(2 * time.Second)
				pingMu.Lock()
				idle := time.Since(lastPing)
				pingMu.Unlock()
				if idle > 8*time.Second {
					os.Exit(0)
				}
			}
		}()
	}

	if os.Getenv("ELRS_NOBROWSER") == "" {
		openUI(url)
	} else {
		fmt.Println("UI at", url)
	}
	select {} // keep running; heartbeat goroutine calls os.Exit
}

// openUI launches the local UI as a chromeless Edge "app" window, falling back
// to the default browser.
func openUI(url string) {
	for _, edge := range []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	} {
		if _, err := os.Stat(edge); err == nil {
			profile := filepath.Join(os.TempDir(), "elrs-updater-ui")
			cmd := exec.Command(edge, "--app="+url,
				"--window-size=460,580",
				"--user-data-dir="+profile)
			if cmd.Start() == nil {
				return
			}
		}
	}
	// Fallback: default browser (opens a normal tab).
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func cleanMsg(s string) string {
	return strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<b>", "", "</b>", "").Replace(s)
}

// --------------------------------------------------------------------- CLI mode

func runCLI() {
	var binPath string
	force := false
	for _, a := range os.Args[1:] {
		if a == "--force" {
			force = true
		} else if !strings.HasPrefix(a, "-") {
			binPath = a
		}
	}
	if binPath == "" {
		fmt.Println("usage: elrs-updater <firmware.bin> [--force]")
		os.Exit(2)
	}
	data, err := os.ReadFile(binPath)
	if err != nil {
		fmt.Println("ERROR: cannot read file:", err)
		os.Exit(2)
	}

	fmt.Printf("Connecting to receiver at %s ...\n", host)
	t, err := getTarget(host, 4*time.Second)
	if err != nil {
		fmt.Println("ERROR: no ELRS receiver found (power RX, connect to 'ExpressLRS RX' wifi).")
		os.Exit(1)
	}
	fmt.Printf("Detected: %s | ver %s | %s/%s\n", t.ProductName, t.Version, t.ModuleType, t.RadioType)

	fmt.Printf("Uploading %s (%d bytes) ...\n", binPath, len(data))
	last := -1
	res, err := uploadFirmware(data, host, force, func(read, total int64) {
		pct := int(read * 100 / total)
		if pct != last {
			last = pct
			fmt.Printf("\r  %3d%%", pct)
		}
	}, 120*time.Second)
	fmt.Println()
	if err != nil {
		fmt.Println("UPLOAD FAILED:", err)
		os.Exit(1)
	}
	msg := cleanMsg(res.Msg)
	switch res.Status {
	case "ok":
		fmt.Println("SUCCESS:", msg)
	case "mismatch":
		fmt.Println("TARGET MISMATCH (safety stop):\n" + msg)
		os.Exit(1)
	default:
		fmt.Println("ERROR:", msg)
		os.Exit(1)
	}
}
