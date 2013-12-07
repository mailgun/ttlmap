package ttlmap

import (
	"fmt"
	"github.com/mailgun/minheap"
	"github.com/mailgun/timetools"
	"time"
)

type mapItem struct {
	key    string
	el     interface{}
	heapEl *minheap.Element
}

type TtlMap struct {
	capacity     int
	items        map[string]*mapItem
	expiryTimes  *minheap.MinHeap
	timeProvider timetools.TimeProvider
}

func NewMap(capacity int) (*TtlMap, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("Capacity should be >= 0")
	}

	return &TtlMap{
		capacity:    capacity,
		items:       make(map[string]*mapItem),
		expiryTimes: minheap.NewMinHeap(),
	}, nil
}

func (m *TtlMap) Set(key string, el interface{}, ttlSeconds int) {
	if len(m.items) > m.capacity {
		m.freeSpace(1)
	}

	expiryTime := int(m.timeProvider.UtcNow().Add(time.Second * time.Duration(ttlSeconds)).Unix())
	item, exists := m.items[key]
	if !exists {
		heapEl := &minheap.Element{
			Priority: expiryTime,
		}
		mapItem := &mapItem{
			key:    key,
			el:     el,
			heapEl: heapEl,
		}
		heapEl.Value = mapItem
		m.items[key] = mapItem
		m.expiryTimes.PushEl(heapEl)
	} else {
		item.el = el
		m.expiryTimes.UpdateEl(item.heapEl, expiryTime)
	}
}

func (m *TtlMap) Get(key string) (interface{}, bool) {
	item, exists := m.items[key]
	if !exists {
		return item, exists
	}
	if m.expireItem(item) {
		return nil, false
	}
	return item.el, true
}

func (m *TtlMap) expireItem(item *mapItem) bool {
	now := int(m.timeProvider.UtcNow().Unix())
	if item.heapEl.Priority > now {
		return false
	}
	delete(m.items, item.key)
	m.expiryTimes.RemoveEl(item.heapEl)
	return true
}

func (m *TtlMap) freeSpace(items int) {
	removed := m.removeExpiredItems(items)
	if removed >= items {
		return
	}
	m.removeLeastUsedItems(items)
}

func (m *TtlMap) removeExpiredItems(iterations int) int {
	removed := 0
	now := int(m.timeProvider.UtcNow().Unix())
	for i := 0; i < iterations; i += 1 {
		if len(m.items) == 0 {
			break
		}
		heapItem := m.expiryTimes.PeekEl()
		if heapItem.Priority > now {
			break
		}
		m.expiryTimes.PopEl()
		mapItem := heapItem.Value.(*mapItem)
		delete(m.items, mapItem.key)
		removed += 1
	}
	return removed
}

func (m *TtlMap) removeLeastUsedItems(iterations int) {
	for i := 0; i < iterations; i += 1 {
		if len(m.items) == 0 {
			return
		}
		heapItem := m.expiryTimes.PopEl()
		mapItem := heapItem.Value.(*mapItem)
		delete(m.items, mapItem.key)
	}
}
