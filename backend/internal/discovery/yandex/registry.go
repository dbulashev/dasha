package yandex

import "sync"

// Registry maps a Yandex folder_id to its lazily-built SDK so HTTP handlers
// can reuse the same credentials the discovery engine uses. The discovery
// engine remains the owner of each SDK's lifecycle; the registry only stores
// references for later lookup.
type Registry struct {
	mu   sync.RWMutex
	sdks map[string]*SDK // key: folder_id
}

// NewRegistry creates an empty SDK registry.
func NewRegistry() *Registry {
	return &Registry{
		mu:   sync.RWMutex{},
		sdks: make(map[string]*SDK),
	}
}

// Register stores the SDK for the given folder_id, overwriting any previous one.
func (r *Registry) Register(folderID string, sdk *SDK) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sdks[folderID] = sdk
}

// Get returns the SDK registered for the folder_id, if any.
func (r *Registry) Get(folderID string) (*SDK, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sdk, ok := r.sdks[folderID]

	return sdk, ok
}
