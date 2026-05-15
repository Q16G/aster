package tui

import (
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

type CredentialStore struct {
	path string
	mu   sync.RWMutex
	data map[string]string
}

func NewCredentialStore(path string) *CredentialStore {
	s := &CredentialStore{
		path: path,
		data: make(map[string]string),
	}
	s.load()
	return s
}

func (s *CredentialStore) Get(providerID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[providerID]
}

func (s *CredentialStore) Set(providerID, apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[providerID] = apiKey
	return s.save()
}

func (s *CredentialStore) Delete(providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, providerID)
	return s.save()
}

func (s *CredentialStore) load() {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var data map[string]string
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return
	}
	if data != nil {
		s.data = data
	}
}

func (s *CredentialStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	raw, err := yaml.Marshal(s.data)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0600)
}
