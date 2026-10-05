package patch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/ernie/trinity-installer/internal/release"
)

const (
	EULAURL = "https://trinity.run/quake3-eula.txt"
	ZipURL  = "https://trinity.run/downloads/quake3-1.32-pk3s.zip"
)

func FetchEULA(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the license text returned HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func FetchZip(ctx context.Context, client *http.Client, url string, progress func(done, total int64)) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the patch download returned HTTP %d", resp.StatusCode)
	}
	var out []byte
	chunk := make([]byte, 256<<10)
	for {
		n, err := resp.Body.Read(chunk)
		out = append(out, chunk[:n]...)
		if progress != nil {
			progress(int64(len(out)), resp.ContentLength)
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

type Set struct{ entries map[string]release.Entry }

// OpenSet accepts the tracker's zip (baseq3/pakN.pk3, missionpack/pakN.pk3), with or without a wrapper directory.
func OpenSet(raw []byte) (*Set, error) {
	p, err := release.Open(release.Spec{Kind: release.KindZip, Root: "baseq3/pak1.pk3"}, raw)
	if err != nil {
		return nil, fmt.Errorf("the patch zip is not the 1.32 point release: %w", err)
	}
	s := &Set{entries: map[string]release.Entry{}}
	for _, e := range p.Entries {
		if strings.HasPrefix(e.Rel, "baseq3/pak") || strings.HasPrefix(e.Rel, "missionpack/pak") {
			s.entries[e.Rel] = e
		}
	}
	return s, nil
}

func (s *Set) Entry(rel string) (release.Entry, bool) {
	e, ok := s.entries[rel]
	return e, ok
}

func (s *Set) Rels() []string {
	var out []string
	for k := range s.entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
