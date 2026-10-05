package local

import (
	"fmt"
	"io/fs"
	"sync"
)

// uninstallKey is the per-user Settings > Apps entry, under HKEY_CURRENT_USER.
const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Trinity`

// registryWriter is the part of HKEY_CURRENT_USER the installer touches; tests swap in a recorder.
type registryWriter interface {
	SetString(key, name, value string) error
	SetDWORD(key, name string, value uint32) error
	GetString(key, name string) (string, error)
	DeleteKey(key string) error
}

// defaultRegistry is a variable so tests never reach the real registry.
var defaultRegistry = newRegistry

// InstalledDir is the install folder the uninstall entry records.
func InstalledDir() (string, error) {
	dir, err := defaultRegistry().GetString(uninstallKey, "InstallLocation")
	if err != nil {
		return "", fmt.Errorf("Trinity's uninstall entry was not found: %w", err)
	}
	return dir, nil
}

// SetInstalledDirForTest records an install location in the test registry, as a finished install would.
func SetInstalledDirForTest(dir string) error {
	return defaultRegistry().SetString(uninstallKey, "InstallLocation", dir)
}

// SetRegistryForTest swaps in an in-memory registry, so tests in other packages never write the real HKEY_CURRENT_USER.
func SetRegistryForTest() {
	mem := &memRegistry{keys: map[string]map[string]any{}}
	defaultRegistry = func() registryWriter { return mem }
}

type memRegistry struct {
	mu   sync.Mutex
	keys map[string]map[string]any
}

func (m *memRegistry) set(key, name string, v any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys[key] == nil {
		m.keys[key] = map[string]any{}
	}
	m.keys[key][name] = v
	return nil
}

func (m *memRegistry) SetString(key, name, value string) error       { return m.set(key, name, value) }
func (m *memRegistry) SetDWORD(key, name string, value uint32) error { return m.set(key, name, value) }

func (m *memRegistry) GetString(key, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.keys[key][name].(string)
	if !ok {
		return "", fs.ErrNotExist
	}
	return s, nil
}

func (m *memRegistry) DeleteKey(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.keys[key]; !ok {
		return fs.ErrNotExist
	}
	delete(m.keys, key)
	return nil
}
