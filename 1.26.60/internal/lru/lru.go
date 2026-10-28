// Package lru is a small least-recently-used cache keyed by chunk position.
package lru

import (
	"container/list"
	"sync"
)

// LRU is a least-recently-used cache of values made on first request. It is
// safe for concurrent use; a value asked for by several goroutines at once
// is made once.
type LRU[V any] struct {
	size    int
	mu      sync.Mutex
	entries map[[2]int32]*list.Element
	order   *list.List // front = most recently used
}

type entry[V any] struct {
	pos   [2]int32
	ready chan struct{}
	v     V
}

// New returns a cache holding at most size values.
func New[V any](size int) LRU[V] {
	return LRU[V]{size: size, entries: map[[2]int32]*list.Element{}, order: list.New()}
}

// Remove drops the value at pos.
func (l *LRU[V]) Remove(pos [2]int32) {
	l.mu.Lock()
	if el, ok := l.entries[pos]; ok {
		l.order.Remove(el)
		delete(l.entries, pos)
	}
	l.mu.Unlock()
}

// Get returns the value at pos, calling build for it if it is not cached.
// Callers asking for a value being made wait for it.
func (l *LRU[V]) Get(pos [2]int32, build func() V) V {
	l.mu.Lock()
	if el, ok := l.entries[pos]; ok {
		l.order.MoveToFront(el)
		e, _ := el.Value.(*entry[V])
		l.mu.Unlock()
		if e == nil {
			var zero V
			return zero
		}
		<-e.ready
		return e.v
	}
	e := &entry[V]{pos: pos, ready: make(chan struct{})}
	l.entries[pos] = l.order.PushFront(e)
	for l.order.Len() > l.size {
		old := l.order.Back()
		l.order.Remove(old)
		if oe, ok := old.Value.(*entry[V]); ok {
			delete(l.entries, oe.pos)
		}
	}
	l.mu.Unlock()
	e.v = build()
	close(e.ready)
	return e.v
}
