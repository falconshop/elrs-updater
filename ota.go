package main

// ELRS receiver WiFi OTA core (Go port of elrs_ota.py).
// Stdlib only so `go build` needs no network and the exe has no dependencies.

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// bindingUID derives the 6-byte ELRS binding UID from a binding phrase, exactly
// as the receiver web UI does: first 6 bytes of MD5(`-DMY_BINDING_PHRASE="<phrase>"`).
func bindingUID(phrase string) []int {
	sum := md5.Sum([]byte(`-DMY_BINDING_PHRASE="` + phrase + `"`))
	uid := make([]int, 6)
	for i := 0; i < 6; i++ {
		uid[i] = int(sum[i])
	}
	return uid
}

// setBindingPhrase sets the receiver's binding UID from a phrase, preserving all
// other settings (reads /config, swaps only the uid, writes it back). The POST to
// /config expects pwm as a raw int array while GET returns pwm objects, so we
// transform it.
func setBindingPhrase(host, phrase string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("http://" + host + "/config")
	if err != nil {
		return fmt.Errorf("config read: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("config parse: %w", err)
	}
	cfg, _ := doc["config"].(map[string]any)
	if cfg == nil {
		return fmt.Errorf("receiver returned no config")
	}

	body := map[string]any{"uid": bindingUID(phrase)}
	for _, k := range []string{"serial-protocol", "sbus-failsafe", "modelid", "force-tlm", "vbind"} {
		if v, ok := cfg[k]; ok {
			body[k] = v
		}
	}
	// pwm: [{config,pin,features}, ...] -> [config, ...]
	if pwm, ok := cfg["pwm"].([]any); ok {
		raws := make([]any, 0, len(pwm))
		for _, p := range pwm {
			if po, ok := p.(map[string]any); ok {
				if c, ok := po["config"]; ok {
					raws = append(raws, c)
					continue
				}
			}
			raws = append(raws, 0)
		}
		body["pwm"] = raws
	}

	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "http://"+host+"/config", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	pr, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("config write: %w", err)
	}
	defer pr.Body.Close()
	if pr.StatusCode != 200 {
		msg, _ := io.ReadAll(pr.Body)
		return fmt.Errorf("config write status %d: %s", pr.StatusCode, string(msg))
	}
	return nil
}

const defaultHost = "10.0.0.1"

// Target mirrors the JSON returned by GET /target on the receiver.
type Target struct {
	TargetName  string `json:"target"`
	Version     string `json:"version"`
	ProductName string `json:"product_name"`
	LuaName     string `json:"lua_name"`
	ModuleType  string `json:"module-type"`
	RadioType   string `json:"radio-type"`
	RegDomain   string `json:"reg_domain"`
}

// getTarget queries the receiver. A non-nil result means we are connected to an
// ELRS device's WiFi AP and it answered.
func getTarget(host string, timeout time.Duration) (*Target, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("http://" + host + "/target")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var t Target
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, fmt.Errorf("bad /target response: %w", err)
	}
	return &t, nil
}

// progressReader wraps a reader and reports how many bytes have been consumed.
type progressReader struct {
	r     io.Reader
	total int64
	read  int64
	cb    func(read, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.cb != nil {
		p.cb(p.read, p.total)
	}
	return n, err
}

func buildMultipart(field, filename string, data []byte) (string, []byte, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename) // sets Content-Type: application/octet-stream
	if err != nil {
		return "", nil, err
	}
	if _, err := part.Write(data); err != nil {
		return "", nil, err
	}
	if err := w.Close(); err != nil {
		return "", nil, err
	}
	return w.FormDataContentType(), buf.Bytes(), nil
}

// uploadResult is the parsed JSON from POST /update.
type uploadResult struct {
	Status string `json:"status"` // "ok" | "mismatch" | "error"
	Msg    string `json:"msg"`
}

// uploadFirmware POSTs the firmware image to /update, reporting upload progress
// via cb(read,total). Returns the receiver's status and message.
func uploadFirmware(data []byte, host string, force bool, cb func(read, total int64), timeout time.Duration) (uploadResult, error) {
	if len(data) == 0 {
		return uploadResult{}, fmt.Errorf("firmware image is empty")
	}
	ct, body, err := buildMultipart("data", "firmware.bin", data)
	if err != nil {
		return uploadResult{}, err
	}

	url := "http://" + host + "/update"
	if force {
		url += "?force=1"
	}

	pr := &progressReader{r: bytes.NewReader(body), total: int64(len(body)), cb: cb}
	req, err := http.NewRequest("POST", url, pr)
	if err != nil {
		return uploadResult{}, err
	}
	// Explicit length so the request is NOT chunked (AsyncWebServer wants a length).
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-FileSize", fmt.Sprintf("%d", len(data)))
	req.Header.Set("Connection", "close")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return uploadResult{}, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var res uploadResult
	if err := json.Unmarshal(respBody, &res); err != nil {
		return uploadResult{Status: "error", Msg: string(respBody)}, nil
	}
	return res, nil
}
