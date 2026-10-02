package kernel

import (
	"sync"
	"time"
)

type Event struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
	Time time.Time      `json:"time"`
}

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
}

func NewEventBus() *EventBus {
	return &EventBus{subscribers: make(map[string][]chan Event)}
}

func (b *EventBus) Subscribe(eventType string) <-chan Event {
	ch := make(chan Event, 32)
	b.mu.Lock()
	b.subscribers[eventType] = append(b.subscribers[eventType], ch)
	b.mu.Unlock()
	return ch
}

func (b *EventBus) Publish(e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b.mu.RLock()
	subs := append([]chan Event(nil), b.subscribers[e.Type]...)
	b.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
			// Slow subscriber: drop rather than block the caller.
		}
	}
}
