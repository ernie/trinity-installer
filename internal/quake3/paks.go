package quake3

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Pak struct {
	Rel  string
	Path string
	Size int64
}

type Validation struct {
	HasBaseq3          bool
	MissingBaseq3      []string
	HasMissionpack     bool
	MissingMissionpack []string
	Paks               []Pak
}

// Retail pak0 is 479 MB; anything smaller is a stub or a wrong folder.
const MinPak0Size = 400 << 20

func Validate(dir string) (Validation, error) {
	var v Validation
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return v, fmt.Errorf("%s is not a folder", dir)
	}
	base, missing := scan(dir, "baseq3", 8, MinPak0Size)
	v.HasBaseq3 = base != nil
	if v.HasBaseq3 {
		v.MissingBaseq3 = missing
		v.Paks = append(v.Paks, base...)
	}
	mp, missing := scan(dir, "missionpack", 3, 1)
	v.HasMissionpack = mp != nil
	if v.HasMissionpack {
		v.MissingMissionpack = missing
		v.Paks = append(v.Paks, mp...)
	}
	return v, nil
}

// scan returns nil when pak0 is absent or smaller than minPak0.
func scan(dir, sub string, last int, minPak0 int64) (paks []Pak, missing []string) {
	for i := 0; i <= last; i++ {
		rel := fmt.Sprintf("%s/pak%d.pk3", sub, i)
		p := filepath.Join(dir, filepath.FromSlash(rel))
		st, err := os.Stat(p)
		if i == 0 {
			if err != nil || st.Size() < minPak0 {
				return nil, nil
			}
		}
		if err != nil {
			missing = append(missing, fmt.Sprintf("pak%d.pk3", i))
			continue
		}
		paks = append(paks, Pak{Rel: rel, Path: p, Size: st.Size()})
	}
	return paks, missing
}

func (v Validation) Baseq3Complete() bool { return v.HasBaseq3 && len(v.MissingBaseq3) == 0 }

func (v Validation) MissionpackComplete() bool { return len(v.MissingMissionpack) == 0 }

func (v Validation) Selected(includeMissionpack bool) []Pak {
	var out []Pak
	for _, p := range v.Paks {
		if includeMissionpack || strings.HasPrefix(p.Rel, "baseq3/") {
			out = append(out, p)
		}
	}
	return out
}
