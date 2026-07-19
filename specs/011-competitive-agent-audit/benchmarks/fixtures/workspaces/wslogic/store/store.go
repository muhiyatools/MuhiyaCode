package store

import "sort"

// Store is a tiny in-memory key/value store.
type Store struct {
	data map[string]string
}

// New returns an empty Store.
func New() *Store {
	return &Store{data: make(map[string]string)}
}

// Set stores value under key.
func (s *Store) Set(key, value string) { s.data[key] = value }

// Get returns the value for key and whether it exists.
func (s *Store) Get(key string) (string, bool) {
	v, ok := s.data[key]
	return v, ok
}

// Delete removes key from the store.
func (s *Store) Delete(key string) { delete(s.data, key) }

// Keys returns all keys in sorted order.
func (s *Store) Keys() []string {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
