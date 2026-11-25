package credentials

import "github.com/zalando/go-keyring"

type keyringBackend struct{}

func NewCurrent(service string) (*Store, error) {
	return New(Options{Backend: keyringBackend{}, Service: service})
}

func (keyringBackend) Get(service string, key string) (string, error) {
	value, err := keyring.Get(service, key)
	if err == keyring.ErrNotFound {
		return "", ErrNotFound
	}
	return value, err
}

func (keyringBackend) Set(service string, key string, value string) error {
	return keyring.Set(service, key, value)
}

func (keyringBackend) Delete(service string, key string) error {
	err := keyring.Delete(service, key)
	if err == keyring.ErrNotFound {
		return ErrNotFound
	}
	return err
}
