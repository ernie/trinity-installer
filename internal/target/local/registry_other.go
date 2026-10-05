//go:build !windows

package local

import "io/fs"

// noRegistry stands in off Windows, where nothing writes an uninstall entry.
type noRegistry struct{}

func newRegistry() registryWriter { return noRegistry{} }

func (noRegistry) SetString(string, string, string) error   { return nil }
func (noRegistry) SetDWORD(string, string, uint32) error    { return nil }
func (noRegistry) GetString(string, string) (string, error) { return "", fs.ErrNotExist }
func (noRegistry) DeleteKey(string) error                   { return nil }
