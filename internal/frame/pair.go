package frame

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func Lookup(ctx context.Context, client *http.Client, host string) (Headset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, devkitURL(host, "/login-name"), nil)
	if err != nil {
		return Headset{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Headset{}, fmt.Errorf("no devkit service at %s (is Developer Mode on?): %w", host, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	login := strings.TrimSpace(string(body))
	if resp.StatusCode != http.StatusOK || login == "" {
		return Headset{}, fmt.Errorf("devkit service at %s answered HTTP %d", host, resp.StatusCode)
	}
	return Headset{Host: host, Login: login}, nil
}

// Register asks the headset to accept the key; it blocks until the user answers the dialog on the device.
func Register(ctx context.Context, client *http.Client, h Headset, pubLine string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, devkitURL(h.Host, "/register"), strings.NewReader(pubLine))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("pairing request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusOK && strings.Contains(string(body), "Registered") {
		return nil
	}
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return fmt.Errorf("the headset refused the pairing: %s", e.Error)
	}
	return fmt.Errorf("pairing answered HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}
