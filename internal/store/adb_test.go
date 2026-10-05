package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ernie/trinity-installer/internal/adb"
)

// fakeADB records each adb argv and answers from reply, keyed by a substring of the joined argv.
type fakeADB struct {
	argv   [][]string
	pushed map[string]string
	reply  map[string]string
	fail   map[string]string
}

func newFakeADB() *fakeADB {
	return &fakeADB{pushed: map[string]string{}, reply: map[string]string{}, fail: map[string]string{}}
}

func (f *fakeADB) adb() *adb.ADB {
	return adb.Fake(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.argv = append(f.argv, args)
		line := strings.Join(args, " ")
		if len(args) == 5 && args[2] == "push" {
			b, err := os.ReadFile(args[3])
			if err != nil {
				return nil, err
			}
			f.pushed[args[4]] = string(b)
		}
		for k, out := range f.fail {
			if strings.Contains(line, k) {
				return []byte(out), errors.New("exit status 1")
			}
		}
		for k, out := range f.reply {
			if strings.Contains(line, k) {
				return []byte(out), nil
			}
		}
		return nil, nil
	})
}

func (f *fakeADB) last() string { return strings.Join(f.argv[len(f.argv)-1], " ") }

func TestADBStat(t *testing.T) {
	f := newFakeADB()
	f.reply["stat -c %s /sdcard/Trinity/baseq3/pak0.pk3"] = "479493658\n"
	s := ADB(f.adb(), "S1")
	size, exists, err := s.Stat("/sdcard/Trinity/baseq3/pak0.pk3")
	if err != nil || !exists || size != 479493658 {
		t.Fatalf("%d %v %v", size, exists, err)
	}
	if f.last() != "-s S1 shell stat -c %s /sdcard/Trinity/baseq3/pak0.pk3" {
		t.Fatal(f.last())
	}
}

func TestADBStatMissing(t *testing.T) {
	f := newFakeADB()
	f.fail["pak1.pk3"] = "stat: '/sdcard/Trinity/baseq3/pak1.pk3': No such file or directory\n"
	_, exists, err := ADB(f.adb(), "S1").Stat("/sdcard/Trinity/baseq3/pak1.pk3")
	if err != nil || exists {
		t.Fatalf("%v %v", exists, err)
	}
}

func TestADBStatFails(t *testing.T) {
	f := newFakeADB()
	f.fail["stat"] = "error: device offline\n"
	if _, _, err := ADB(f.adb(), "S1").Stat("/sdcard/Trinity/baseq3/pak1.pk3"); err == nil {
		t.Fatal("want an error from an offline device")
	}
}

func TestADBFreeSpace(t *testing.T) {
	f := newFakeADB()
	f.reply["df -k /sdcard/Trinity"] = "Filesystem     1K-blocks     Used Available Use% Mounted on\n/dev/fuse      110324296 52731188  57461652  48% /storage/emulated\n"
	n, err := ADB(f.adb(), "S1").FreeSpace(context.Background(), "/sdcard/Trinity")
	if err != nil || n != 57461652*1024 {
		t.Fatalf("%d %v", n, err)
	}
}

func TestADBPutPushesTheReader(t *testing.T) {
	f := newFakeADB()
	err := ADB(f.adb(), "S1").Put(context.Background(), "/sdcard/Trinity/baseq3/pak0.pk3", strings.NewReader("pak bytes"), 9, 0o644)
	if err != nil || f.pushed["/sdcard/Trinity/baseq3/pak0.pk3"] != "pak bytes" {
		t.Fatalf("%v %v", err, f.pushed)
	}
	if tmp := f.argv[0][3]; fileExists(tmp) {
		t.Fatalf("temp file %s left behind", tmp)
	}
}

func TestADBMkdirAllAndJoin(t *testing.T) {
	f := newFakeADB()
	s := ADB(f.adb(), "S1")
	if err := s.MkdirAll("/sdcard/Trinity/my paks"); err != nil {
		t.Fatal(err)
	}
	if f.last() != "-s S1 shell mkdir -p '/sdcard/Trinity/my paks'" {
		t.Fatal(f.last())
	}
	if j := s.Join("/sdcard/Trinity", "baseq3", "pak0.pk3"); j != "/sdcard/Trinity/baseq3/pak0.pk3" {
		t.Fatal(j)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
