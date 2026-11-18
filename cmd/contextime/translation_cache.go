package main

import (
	"container/list"
	"sync"
)

const translationCacheEntries = 128

type translationCacheKey struct {
	source, language, engine, sourceLanguage string
}

type translationCacheEntry struct {
	key   translationCacheKey
	value string
}

type translationMemory struct {
	mu    sync.Mutex
	order list.List
	items map[translationCacheKey]*list.Element
}

var sessionTranslations translationMemory

func (m *translationMemory) get(key translationCacheKey) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if element := m.items[key]; element != nil {
		m.order.MoveToFront(element)
		return element.Value.(translationCacheEntry).value
	}
	return ""
}

func (m *translationMemory) set(key translationCacheKey, value string) {
	if value == "" || len(value) > 4096 || len(key.source) > 4096 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := translationCacheEntry{key: key, value: value}
	if element := m.items[key]; element != nil {
		element.Value = entry
		m.order.MoveToFront(element)
		return
	}
	if m.items == nil {
		m.items = make(map[translationCacheKey]*list.Element)
	}
	m.items[key] = m.order.PushFront(entry)
	if m.order.Len() > translationCacheEntries {
		last := m.order.Back()
		delete(m.items, last.Value.(translationCacheEntry).key)
		m.order.Remove(last)
	}
}
