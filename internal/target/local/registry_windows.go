package local

import "golang.org/x/sys/windows/registry"

type winRegistry struct{}

func newRegistry() registryWriter { return winRegistry{} }

func (winRegistry) SetString(key, name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

func (winRegistry) SetDWORD(key, name string, value uint32) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetDWordValue(name, value)
}

func (winRegistry) GetString(key, name string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	s, _, err := k.GetStringValue(name)
	return s, err
}

func (winRegistry) DeleteKey(key string) error {
	return registry.DeleteKey(registry.CURRENT_USER, key)
}
