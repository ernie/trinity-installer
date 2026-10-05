package release

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const DefaultAPI = "https://api.github.com/repos/ernie/trinity-engine/releases/latest"
const AssetName = "trinity-frame-arm64.zip"

type Asset struct {
	Tag  string
	Name string
	Size int64
	URL  string
}

func Latest(ctx context.Context, client *http.Client, api string) (Asset, error) {
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
	for _, a := range body.Assets {
		if a.Name == AssetName {
			return Asset{Tag: body.Tag, Name: a.Name, Size: a.Size, URL: a.URL}, nil
		}
	}
	return Asset{}, fmt.Errorf("release %s has no %s", body.Tag, AssetName)
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
	return buf.Bytes(), nil
}

func OpenZip(b []byte) (*zip.Reader, error) {
	return zip.NewReader(bytes.NewReader(b), int64(len(b)))
}
