package backup

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryStore is an in-memory ObjectStore for tests. The Fail hooks, when
// set, can reject individual operations.
type MemoryStore struct {
	mu      sync.Mutex
	objects map[string][]byte

	FailPut    func(key string) error
	FailGet    func(key string) error
	FailList   func(prefix string) error
	FailDelete func(key string) error
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{objects: make(map[string][]byte)}
}

func (m *MemoryStore) Put(ctx context.Context, key string, body io.Reader, _ string) error {
	if m.FailPut != nil {
		if err := m.FailPut(key); err != nil {
			return err
		}
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	m.objects[key] = data
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if m.FailGet != nil {
		if err := m.FailGet(key); err != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	data, ok := m.objects[key]
	m.mu.Unlock()
	if !ok {
		return nil, ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *MemoryStore) List(_ context.Context, prefix string) ([]Object, error) {
	if m.FailList != nil {
		if err := m.FailList(prefix); err != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	objects := make([]Object, 0)
	for key, data := range m.objects {
		if strings.HasPrefix(key, prefix) {
			objects = append(objects, Object{Key: key, Size: int64(len(data)), LastModified: time.Time{}})
		}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	return objects, nil
}

func (m *MemoryStore) Delete(_ context.Context, key string) error {
	if m.FailDelete != nil {
		if err := m.FailDelete(key); err != nil {
			return err
		}
	}
	m.mu.Lock()
	delete(m.objects, key)
	m.mu.Unlock()
	return nil
}

// Keys returns every stored key in order.
func (m *MemoryStore) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.objects))
	for key := range m.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Object returns a stored object's content.
func (m *MemoryStore) Object(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	return data, ok
}
