package install

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sort"

	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/store"
	"github.com/ernie/trinity-installer/internal/target"
)

type memStore struct {
	files map[string][]byte
	modes map[string]os.FileMode
	free  int64
	puts  int
	// failPut fails the next put of a path once; onFreeSpace runs inside the free-space check.
	failPut     map[string]error
	onFreeSpace func()
}

func newMemStore() *memStore {
	return &memStore{files: map[string][]byte{}, modes: map[string]os.FileMode{}, free: 1 << 40, failPut: map[string]error{}}
}

func (m *memStore) Join(elem ...string) string { return path.Join(elem...) }
func (m *memStore) MkdirAll(string) error      { return nil }
func (m *memStore) Stat(p string) (int64, bool, error) {
	b, ok := m.files[p]
	return int64(len(b)), ok, nil
}
func (m *memStore) FreeSpace(context.Context, string) (int64, error) {
	if m.onFreeSpace != nil {
		m.onFreeSpace()
	}
	return m.free, nil
}
func (m *memStore) Put(ctx context.Context, p string, r io.Reader, size int64, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err, ok := m.failPut[p]; ok {
		delete(m.failPut, p)
		return err
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.files[p], m.modes[p] = b, mode
	m.puts++
	return nil
}
func (m *memStore) dump() string {
	var keys []string
	for k := range m.files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	for _, k := range keys {
		fmt.Fprintf(&b, "%s %o %d\n", k, m.modes[k], len(m.files[k]))
	}
	return b.String()
}

// fakeTarget records hook calls; Fail makes a named hook fail once.
type fakeTarget struct {
	st         *memStore
	steps      []target.Step
	calls      []string
	Fail       map[string]error
	appID      uint32
	paksDir    string
	spec       release.Spec
	reconnects int
}

func newFakeTarget(steps ...target.Step) *fakeTarget {
	return &fakeTarget{st: newMemStore(), steps: steps, Fail: map[string]error{}, appID: 42, paksDir: "/dest", spec: release.Spec{Kind: release.KindZip, Root: "trinity"}}
}

func (f *fakeTarget) hook(name string) error {
	f.calls = append(f.calls, name)
	if err, ok := f.Fail[name]; ok {
		delete(f.Fail, name)
		return err
	}
	return nil
}
func (f *fakeTarget) Name() string                                      { return "fake" }
func (f *fakeTarget) Applicable(context.Context) ([]target.Step, error) { return f.steps, nil }
func (f *fakeTarget) Store() store.Store                                { return f.st }
func (f *fakeTarget) Asset() release.Spec                               { return f.spec }
func (f *fakeTarget) PrepareDestination(context.Context, func(string)) (string, error) {
	return f.paksDir, f.hook("prepare")
}
func (f *fakeTarget) PushPackage(ctx context.Context, pkg *release.Package, log func(string)) error {
	if err := f.hook("package"); err != nil {
		return err
	}
	for _, e := range pkg.Entries {
		rc, err := e.Open()
		if err != nil {
			return err
		}
		err = f.st.Put(ctx, f.st.Join(f.paksDir, e.Rel), rc, e.Size(), 0o755)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeTarget) RegisterLaunchEntry(context.Context, func(string)) error {
	return f.hook("launch")
}
func (f *fakeTarget) ReadAppID(context.Context, func(string)) (uint32, error) {
	return f.appID, f.hook("appid")
}
func (f *fakeTarget) RegisterVR(_ context.Context, id uint32, _ map[string][]byte, _ func(string)) error {
	return f.hook(fmt.Sprintf("vr:%d", id))
}
func (f *fakeTarget) InstallArtwork(_ context.Context, id uint32, art map[string][]byte, _ func(string)) error {
	return f.hook(fmt.Sprintf("art:%d:%d", id, len(art)))
}
func (f *fakeTarget) Done() string                    { return "done" }
func (f *fakeTarget) Reconnect(context.Context) error { f.reconnects++; return nil }
