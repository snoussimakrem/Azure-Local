package kernel

import (
	"fmt"
	"sort"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	services map[string]Service
}

func NewRegistry() *Registry {
	return &Registry{services: make(map[string]Service)}
}

func (r *Registry) Register(s Service) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := s.Name()
	if _, exists := r.services[name]; exists {
		return fmt.Errorf("service %q already registered", name)
	}
	r.services[name] = s
	return nil
}

func (r *Registry) Get(name string) (Service, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.services[name]
	return s, ok
}

func (r *Registry) All() []Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Service, 0, len(r.services))
	for _, s := range r.services {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
