package ttlmap

import (
	"fmt"
	"sync"
	"time"

	"github.com/mailgun/minheap"
	"github.com/mailgun/timetools"
)

type TtlMapOption func(m *TtlMap) error

// Clock sets the time provider clock, handy for testing
func Clock(c timetools.TimeProvider) TtlMapOption {
	return func(m *TtlMap) error {
		m.TimeProvider = c
		return nil
	}
}

type Callback func(key string, el interface{})

// CallOnExpire will call this callback on expiration of elements
func CallOnExpire(cb Callback) TtlMapOption {
	return func(m *TtlMap) error {
		m.onExpire = cb
		return nil
	}
}

type TtlMap struct {
	capacity     int
	elements     map[string]*mapElement
	expiryTimes  *minheap.MinHeap
	TimeProvider timetools.TimeProvider
	// onExpire callback will be called when element is expired
	onExpire Callback
	// Mutex to keep things thread safe
	lock *sync.RWMutex
}

type mapElement struct {
	key    string
	value  interface{}
	heapEl *minheap.Element
}

func NewMap(capacity int, opts ...TtlMapOption) (*TtlMap, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("Capacity should be > 0")
	}

	m := &TtlMap{
		capacity:    capacity,
		elements:    make(map[string]*mapElement),
		expiryTimes: minheap.NewMinHeap(),
		lock:        &sync.RWMutex{},
	}

	for _, o := range opts {
		if err := o(m); err != nil {
			return nil, err
		}
	}

	if m.TimeProvider == nil {
		m.TimeProvider = &timetools.RealTime{}
	}

	return m, nil
}

func NewMapWithProvider(capacity int, timeProvider timetools.TimeProvider) (*TtlMap, error) {
	if timeProvider == nil {
		return nil, fmt.Errorf("Please pass timeProvider")
	}
	return NewMap(capacity, Clock(timeProvider))
}

func (m *TtlMap) Set(key string, value interface{}, ttlSeconds int) error {
	// Lock for writes
	m.lock.Lock()
	defer m.lock.Unlock()

	expiryTime, err := m.toEpochSeconds(ttlSeconds)
	if err != nil {
		return err
	}

	mapEl, exists := m.elements[key]
	if !exists {
		if len(m.elements) >= m.capacity {
			m.freeSpace(1)
		}
		heapEl := &minheap.Element{
			Priority: expiryTime,
		}
		mapEl := &mapElement{
			key:    key,
			value:  value,
			heapEl: heapEl,
		}
		heapEl.Value = mapEl
		m.elements[key] = mapEl
		m.expiryTimes.PushEl(heapEl)
	} else {
		mapEl.value = value
		m.expiryTimes.UpdateEl(mapEl.heapEl, expiryTime)
	}
	return nil
}

func (m *TtlMap) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return len(m.elements)
}

func (m *TtlMap) Get(key string) (interface{}, bool) {
	mapEl, exists := m.get(key)
	if !exists {
		return nil, false
	}
	if m.shouldExpire(mapEl) {
		m.expireElement(mapEl)
		return nil, false
	}
	return mapEl.value, true
}

func (m *TtlMap) get(key string) (*mapElement, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	mapEl, exists := m.elements[key]
	return mapEl, exists
}

func (m *TtlMap) Increment(key string, value int, ttlSeconds int) (int, error) {
	expiryTime, err := m.toEpochSeconds(ttlSeconds)
	if err != nil {
		return 0, err
	}

	mapEl, exists := m.get(key)
	if !exists {
		m.Set(key, value, ttlSeconds)
		return value, nil
	}
	if m.shouldExpire(mapEl) {
		m.Set(key, value, ttlSeconds)
		return value, nil
	}
	return m.increment(mapEl, value, expiryTime)
}

func (m *TtlMap) increment(mapEl *mapElement, value int, expiryTime int) (int, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	currentValue, ok := mapEl.value.(int)
	if !ok {
		return 0, fmt.Errorf("Expected existing value to be integer, got %T", mapEl.value)
	}
	currentValue += value
	mapEl.value = currentValue

	m.expiryTimes.UpdateEl(mapEl.heapEl, expiryTime)
	return currentValue, nil
}

func (m *TtlMap) GetInt(key string) (int, bool, error) {
	valueI, exists := m.Get(key)
	if !exists {
		return 0, false, nil
	}
	value, ok := valueI.(int)
	if !ok {
		return 0, false, fmt.Errorf("Expected existing value to be integer, got %T", valueI)
	}
	return value, true, nil
}

func (m *TtlMap) expireElement(mapEl *mapElement) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	// Ensure key hasn't already been removed by another thread
	if _, exists := m.elements[mapEl.key]; !exists {
		return false
	}

	if m.onExpire != nil {
		m.onExpire(mapEl.key, mapEl.value)
	}

	delete(m.elements, mapEl.key)
	m.expiryTimes.RemoveEl(mapEl.heapEl)
	return true
}

func (m *TtlMap) shouldExpire(mapEl *mapElement) bool {
	return mapEl.heapEl.Priority <= m.now()
}

func (m *TtlMap) now() int {
	return int(m.TimeProvider.UtcNow().Unix())
}

func (m *TtlMap) toEpochSeconds(ttlSeconds int) (int, error) {
	if ttlSeconds <= 0 {
		return 0, fmt.Errorf("ttlSeconds should be >= 0, got %d", ttlSeconds)
	}
	return int(m.TimeProvider.UtcNow().Add(time.Second * time.Duration(ttlSeconds)).Unix()), nil
}

func (m *TtlMap) freeSpace(count int) {
	removed := m.removeExpired(count)
	if removed >= count {
		return
	}
	m.removeLastUsed(count - removed)
}

func (m *TtlMap) removeExpired(iterations int) int {
	removed := 0
	now := m.now()
	for i := 0; i < iterations; i += 1 {
		if len(m.elements) == 0 {
			break
		}
		heapEl := m.expiryTimes.PeekEl()
		if heapEl.Priority > now {
			break
		}
		m.expiryTimes.PopEl()
		mapEl := heapEl.Value.(*mapElement)
		delete(m.elements, mapEl.key)
		removed += 1
	}
	return removed
}

func (m *TtlMap) removeLastUsed(iterations int) {
	for i := 0; i < iterations; i += 1 {
		if len(m.elements) == 0 {
			return
		}
		heapEl := m.expiryTimes.PopEl()
		mapEl := heapEl.Value.(*mapElement)
		delete(m.elements, mapEl.key)
	}
}
