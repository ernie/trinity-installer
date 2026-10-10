package release

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const sumsAsset = "sha256sums.txt"

type Asset struct {
	Tag     string
	Name    string
	Size    int64
	URL     string
	SumsURL string
}

func Latest(ctx context.Context, client *http.Client, api, asset string) (Asset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return Asset{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Asset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Asset{}, fmt.Errorf("release lookup returned HTTP %d", resp.StatusCode)
	}
	var body struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Asset{}, err
	}
	found := Asset{Tag: body.Tag}
	for _, a := range body.Assets {
		switch a.Name {
		case asset:
			found.Name, found.Size, found.URL = a.Name, a.Size, a.URL
		case sumsAsset:
			found.SumsURL = a.URL
		}
	}
	if found.URL == "" {
		return Asset{}, fmt.Errorf("release %s has no %s", body.Tag, asset)
	}
	if found.SumsURL == "" {
		return Asset{}, fmt.Errorf("release %s has no %s to verify %s against", body.Tag, sumsAsset, asset)
	}
	return found, nil
}

func Download(ctx context.Context, client *http.Client, a Asset, progress func(done int64)) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	buf := bytes.NewBuffer(make([]byte, 0, a.Size))
	chunk := make([]byte, 256<<10)
	for {
		n, err := resp.Body.Read(chunk)
		buf.Write(chunk[:n])
		if progress != nil {
			progress(int64(buf.Len()))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if int64(buf.Len()) != a.Size {
		return nil, fmt.Errorf("downloaded %d bytes, release lists %d", buf.Len(), a.Size)
	}
	if err := verify(ctx, client, a, buf.Bytes()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func verify(ctx context.Context, client *http.Client, a Asset, b []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.SumsURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s download returned HTTP %d", sumsAsset, resp.StatusCode)
	}
	sums, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	want, ok := checksumFor(sums, a.Name)
	if !ok {
		return fmt.Errorf("release %s lists no checksum for %s", a.Tag, a.Name)
	}
	got := sha256.Sum256(b)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return fmt.Errorf("%s failed its checksum check; press Retry to download it again", a.Name)
	}
	return nil
}

// checksumFor reads sha256sum output, where a "*" before the name marks binary mode.
func checksumFor(sums []byte, name string) (string, bool) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return fields[0], true
		}
	}
	return "", false
}

func OpenZip(b []byte) (*zip.Reader, error) {
	return zip.NewReader(bytes.NewReader(b), int64(len(b)))
}
