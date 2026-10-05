package quake3

import (
	"fmt"
	"os"
	"path/filepath"
)

type Pak struct {
	Rel  string
	Path string
	Size int64
}

// Dir is one game directory of the source install: pak0 decides Present, the patch paks decide the state.
type Dir struct {
	Present bool
	Paks    []Pak
	Missing []string
}

func (d Dir) State() string {
	switch {
	case !d.Present:
		return "NOT PRESENT"
	case len(d.Missing) > 0:
		return "NEEDS PATCH"
	}
	return "OK"
}

type Validation struct {
	Baseq3      Dir
	Missionpack Dir
}

const MinPak0Size = 400 << 20

func Validate(dir string) (Validation, error) {
	var v Validation
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return v, fmt.Errorf("%s is not a folder", dir)
	}
	v.Baseq3 = scan(dir, "baseq3", 8, MinPak0Size)
	v.Missionpack = scan(dir, "missionpack", 3, 1)
	return v, nil
}

func scan(dir, sub string, last int, minPak0 int64) Dir {
	var d Dir
	for i := 0; i <= last; i++ {
		rel := fmt.Sprintf("%s/pak%d.pk3", sub, i)
		p := filepath.Join(dir, filepath.FromSlash(rel))
		st, err := os.Stat(p)
		if i == 0 {
			if err != nil || st.Size() < minPak0 {
				return Dir{}
			}
			d.Present = true
		}
		if err != nil {
			d.Missing = append(d.Missing, fmt.Sprintf("pak%d.pk3", i))
			continue
		}
		d.Paks = append(d.Paks, Pak{Rel: rel, Path: p, Size: st.Size()})
	}
	return d
}

func (v Validation) Ready() bool { return v.Baseq3.Present }

func (v Validation) LocalPaks() []Pak {
	var out []Pak
	for _, d := range []Dir{v.Baseq3, v.Missionpack} {
		if d.Present {
			out = append(out, d.Paks...)
		}
	}
	return out
}

func (v Validation) NeededPatch() []string {
	var out []string
	for _, pair := range []struct {
		sub string
		d   Dir
	}{{"baseq3", v.Baseq3}, {"missionpack", v.Missionpack}} {
		if !pair.d.Present {
			continue
		}
		for _, m := range pair.d.Missing {
			out = append(out, pair.sub+"/"+m)
		}
	}
	return out
}
