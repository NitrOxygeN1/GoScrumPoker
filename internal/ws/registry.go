package ws

import "sync"

// Registry tracks active WebSocket clients per room for fan-out broadcasts.
type Registry struct {
	mu    sync.RWMutex
	rooms map[string]map[*Client]struct{}
}

// NewRegistry constructs an empty registry.
func NewRegistry() *Registry {
	return &Registry{rooms: make(map[string]map[*Client]struct{})}
}

// Add registers a client to a room.
func (r *Registry) Add(roomID string, c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	set, ok := r.rooms[roomID]
	if !ok {
		set = make(map[*Client]struct{})
		r.rooms[roomID] = set
	}
	set[c] = struct{}{}
}

// Remove unregisters a client from a room.
func (r *Registry) Remove(roomID string, c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if set, ok := r.rooms[roomID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(r.rooms, roomID)
		}
	}
}

// Broadcast delivers a message to every client in a room.
func (r *Registry) Broadcast(roomID string, payload []byte) {
	r.mu.RLock()
	set := r.rooms[roomID]
	clients := make([]*Client, 0, len(set))
	for c := range set {
		clients = append(clients, c)
	}
	r.mu.RUnlock()

	for _, c := range clients {
		c.enqueue(payload)
	}
}
