package install

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/store"
	"github.com/ernie/trinity-installer/internal/target"
)

const spaceMargin = 256 << 20

func fetchRelease(ctx context.Context, t target.Target, st *state, log func(string)) error {
	spec := t.Asset()
	raw, tag, err := st.opts.Fetch(ctx, spec, log)
	if err != nil {
		return err
	}
	pkg, err := release.Open(spec, raw)
	if err != nil {
		return err
	}
	pkg.Tag = tag
	st.pkg = pkg
	log(fmt.Sprintf("%s: %d bytes", spec.Asset, len(raw)))
	return nil
}

type pending struct {
	rel  string
	size int64
	open func() (io.ReadCloser, error)
}

// pushed reports written files to a target that records them, and to nothing otherwise.
func pushed(t target.Target) func(rel string) {
	if r, ok := t.(target.Recorder); ok {
		return r.Pushed
	}
	return func(string) {}
}

func pushAll(ctx context.Context, s store.Store, dir string, items []pending, onPut func(rel string), log func(string)) error {
	var todo []pending
	var need int64
	for _, p := range items {
		size, exists, err := s.Stat(s.Join(dir, p.rel))
		if err != nil {
			return err
		}
		if exists && size == p.size {
			log("already there: " + p.rel)
			continue
		}
		todo = append(todo, p)
		need += p.size
	}
	if len(todo) == 0 {
		return nil
	}
	avail, err := s.FreeSpace(ctx, dir)
	if err != nil {
		return err
	}
	if avail < need+spaceMargin {
		return fmt.Errorf("the destination has %d MB free but the files need %d MB", avail>>20, (need+spaceMargin)>>20)
	}
	for _, p := range todo {
		if err := ctx.Err(); err != nil {
			return err
		}
		rc, err := p.open()
		if err != nil {
			return err
		}
		err = s.Put(ctx, s.Join(dir, p.rel), &progressReader{r: rc, total: p.size, name: p.rel, log: log}, p.size, 0o644)
		rc.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", p.rel, err)
		}
		onPut(p.rel)
		log(fmt.Sprintf("copied %s (%d MB)", p.rel, p.size>>20))
	}
	return nil
}

func pushPaks(ctx context.Context, s store.Store, st *state, onPut func(string), log func(string)) error {
	var items []pending
	for _, p := range st.opts.Paks {
		path := p.Path
		items = append(items, pending{p.Rel, p.Size, func() (io.ReadCloser, error) { return os.Open(path) }})
	}
	return pushAll(ctx, s, st.paksDir, items, onPut, log)
}

func pushPatch(ctx context.Context, s store.Store, st *state, onPut func(string), log func(string)) error {
	if st.opts.Patch == nil {
		log("the 1.32 patch files came from your Quake III folder")
		return nil
	}
	var items []pending
	for _, rel := range st.opts.PatchRels {
		e, ok := st.opts.Patch.Entry(rel)
		if !ok {
			return fmt.Errorf("the 1.32 patch does not contain %s", rel)
		}
		items = append(items, pending{rel, e.Size(), e.Open})
	}
	return pushAll(ctx, s, st.paksDir, items, onPut, log)
}

// progressReader logs every 16 MB so a long upload is visibly alive.
type progressReader struct {
	r     io.Reader
	total int64
	done  int64
	last  int64
	name  string
	log   func(string)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.done-p.last >= 16<<20 {
		p.last = p.done
		p.log(fmt.Sprintf("copied %d of %d MB of %s", p.done>>20, p.total>>20, p.name))
	}
	return n, err
}
