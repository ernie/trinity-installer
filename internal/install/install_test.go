package install

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ernie/trinity-installer/internal/patch"
	"github.com/ernie/trinity-installer/internal/quake3"
	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/target"
)

func zipOf(names ...string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, n := range names {
		f, _ := w.Create(n)
		f.Write([]byte("x " + n))
	}
	w.Close()
	return buf.Bytes()
}

func localPaks(t *testing.T, rels ...string) []quake3.Pak {
	dir := t.TempDir()
	var paks []quake3.Pak
	for _, rel := range rels {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("pak "+rel), 0o644)
		paks = append(paks, quake3.Pak{Rel: rel, Path: p, Size: int64(len("pak " + rel))})
	}
	return paks
}

func options(t *testing.T) Options {
	return Options{
		Fetch: func(context.Context, release.Spec, func(string)) ([]byte, error) {
			return zipOf("trinity", "baseq3/pak8t.pk3"), nil
		},
		Paks: localPaks(t, "baseq3/pak0.pk3", "missionpack/pak0.pk3"),
		Art:  map[string][]byte{"capsule": {1}, "hero": {2}},
	}
}

func collect() (func(Progress), *[]Progress) {
	var all []Progress
	return func(p Progress) { all = append(all, p) }, &all
}

func TestRunFullPlan(t *testing.T) {
	ft := newFakeTarget(target.PushPatch, target.RegisterLaunchEntry, target.ReadAppID, target.RegisterVR, target.InstallArtwork)
	plan, _ := target.Plan(context.Background(), ft)
	report, events := collect()
	if err := Run(context.Background(), ft, plan, options(t), 0, report); err != nil {
		t.Fatalf("%v\n%s", err, ft.st.dump())
	}
	dump := ft.st.dump()
	for _, want := range []string{"/dest/trinity 755", "/dest/baseq3/pak8t.pk3 755", "/dest/baseq3/pak0.pk3 644", "/dest/missionpack/pak0.pk3 644"} {
		if !strings.Contains(dump, want) {
			t.Fatalf("missing %q in\n%s", want, dump)
		}
	}
	if got := strings.Join(ft.calls, ","); got != "prepare,package,launch,appid,vr:42,art:42:2" {
		t.Fatalf("%s", got)
	}
	last := (*events)[len(*events)-1]
	if last.Index != len(plan)-1 || last.Step != target.InstallArtwork || last.State != Done {
		t.Fatalf("%+v", last)
	}
}

func TestRunShortPlanNeverCallsAbsentHooks(t *testing.T) {
	ft := newFakeTarget()
	plan, _ := target.Plan(context.Background(), ft)
	report, _ := collect()
	if err := Run(context.Background(), ft, plan, options(t), 0, report); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ft.calls, ","); got != "prepare,package" {
		t.Fatalf("%s", got)
	}
}

func TestPatchStepMixesLocalAndDownloaded(t *testing.T) {
	ft := newFakeTarget(target.PushPatch)
	plan, _ := target.Plan(context.Background(), ft)
	opts := options(t)
	opts.Paks = localPaks(t, "baseq3/pak0.pk3", "baseq3/pak1.pk3", "missionpack/pak0.pk3")
	set, err := patch.OpenSet(zipOf("baseq3/pak1.pk3", "baseq3/pak2.pk3", "missionpack/pak1.pk3", "missionpack/pak2.pk3"))
	if err != nil {
		t.Fatal(err)
	}
	opts.Patch, opts.PatchRels = set, []string{"missionpack/pak1.pk3", "missionpack/pak2.pk3"}
	report, _ := collect()
	if err := Run(context.Background(), ft, plan, opts, 0, report); err != nil {
		t.Fatal(err)
	}
	dump := ft.st.dump()
	for _, want := range []string{"/dest/baseq3/pak1.pk3 644", "/dest/missionpack/pak1.pk3 644", "/dest/missionpack/pak2.pk3 644"} {
		if !strings.Contains(dump, want) {
			t.Fatalf("missing %q in\n%s", want, dump)
		}
	}
	if strings.Contains(dump, "/dest/baseq3/pak2.pk3") {
		t.Fatal("downloaded a pak that was not needed")
	}
	if string(ft.st.files["/dest/baseq3/pak1.pk3"]) != "pak baseq3/pak1.pk3" {
		t.Fatal("local patch pak was not the one copied")
	}
}

func TestPaksSkipSameSizeAndRespectFreeSpace(t *testing.T) {
	ft := newFakeTarget()
	plan, _ := target.Plan(context.Background(), ft)
	opts := options(t)
	ft.st.files["/dest/baseq3/pak0.pk3"] = []byte("pak baseq3/pak0.pk3")
	report, _ := collect()
	if err := Run(context.Background(), ft, plan, opts, 0, report); err != nil {
		t.Fatal(err)
	}
	if ft.st.puts != 3 { // trinity, pak8t, missionpack/pak0
		t.Fatalf("puts %d", ft.st.puts)
	}
	ft = newFakeTarget()
	ft.st.free = 10
	report, _ = collect()
	err := Run(context.Background(), ft, plan, options(t), 0, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != target.PushRetailPaks || !strings.Contains(err.Error(), "free") {
		t.Fatalf("%v", err)
	}
}

func TestRetryFromFailedStepReconnectsAndCarriesState(t *testing.T) {
	ft := newFakeTarget(target.RegisterLaunchEntry, target.ReadAppID)
	plan, _ := target.Plan(context.Background(), ft)
	opts := options(t)
	opts.Carry = &Carry{}
	ft.Fail["launch"] = errors.New("Steam is not running")
	report, _ := collect()
	err := Run(context.Background(), ft, plan, opts, 0, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != target.RegisterLaunchEntry || se.Index != 4 {
		t.Fatalf("%v", err)
	}
	puts := ft.st.puts
	if err := Run(context.Background(), ft, plan, opts, se.Index, report); err != nil {
		t.Fatal(err)
	}
	if ft.st.puts != puts || ft.reconnects != 1 {
		t.Fatalf("retry re-pushed (%d vs %d) or did not reconnect (%d)", ft.st.puts, puts, ft.reconnects)
	}
	if got := strings.Join(ft.calls, ","); got != "prepare,package,launch,launch,appid" {
		t.Fatalf("%s", got)
	}
}

func TestFetchRejectsBadAsset(t *testing.T) {
	ft := newFakeTarget()
	plan, _ := target.Plan(context.Background(), ft)
	opts := options(t)
	opts.Fetch = func(context.Context, release.Spec, func(string)) ([]byte, error) { return zipOf("other"), nil }
	report, _ := collect()
	err := Run(context.Background(), ft, plan, opts, 0, report)
	var se *StepError
	if !errors.As(err, &se) || se.Step != target.FetchRelease {
		t.Fatalf("%v", err)
	}
}
