package oncecache

import (
	"sync"
)

type StringCache struct {
	Cache
}

func (a *StringCache) Do(f func() (string, error)) (string, error) {
	ret, err := a.Cache.Do(func() (interface{}, error) {
		return f()
	})
	if err != nil {
		return "", err
	}
	return ret.(string), err
}

type Cache struct {
	mu  sync.Mutex
	val interface{}
}

func (a *Cache) Do(f func() (interface{}, error)) (interface{}, error) {
	a.mu.Lock()
	if a.val != nil {
		a.mu.Unlock()
		return a.val, nil
	}
	defer a.mu.Unlock()
	ret, err := f()
	if err == nil {
		a.val = ret
	}
	return ret, err
}
