package cmap

import (
	"errors"
	"sync"
)

var Break = errors.New(":break:")

const DefaultShardCount = 1 << 8 // 256

// ForeachFunc is a function that gets passed to Foreach, returns true to break early
type ForeachFunc func(key string, val interface{}) (BreakEarly bool)

type MapShard struct {
	l sync.RWMutex
	m map[string]interface{}
}

func (ms *MapShard) Set(key string, v interface{}) {
	ms.l.Lock()
	ms.m[key] = v
	ms.l.Unlock()
}

func (ms *MapShard) Get(key string) interface{} {
	ms.l.RLock()
	v := ms.m[key]
	ms.l.RUnlock()
	return v
}

func (ms *MapShard) Has(key string) bool {
	ms.l.RLock()
	_, ok := ms.m[key]
	ms.l.RUnlock()
	return ok
}

func (ms *MapShard) Delete(key string) {
	ms.l.Lock()
	delete(ms.m, key)
	ms.l.Unlock()
}

func (ms *MapShard) DeleteAndGet(key string) interface{} {
	ms.l.Lock()
	v := ms.m[key]
	delete(ms.m, key)
	ms.l.Unlock()
	return v
}

func (ms *MapShard) Len() int {
	ms.l.RLock()
	ln := len(ms.m)
	ms.l.RUnlock()
	return ln
}

func (ms *MapShard) Foreach(fn ForeachFunc) {
	ms.l.RLock()
	for k, v := range ms.m {
		if !fn(k, v) {
			break
		}
	}
	ms.l.RUnlock()
}

type CMap []MapShard

// New is an alias for NewSize(DefaultShardCount)
func New() CMap { return NewSize(DefaultShardCount) }

func NewSize(shardCount int) CMap {
	// must be a power of 2
	if shardCount == 0 {
		shardCount = DefaultShardCount
	} else if shardCount&(shardCount-1) != 0 {
		panic("shardCount must be a power of 2")
	}
	cm := make(CMap, shardCount)
	for i := range cm {
		cm[i].m = make(map[string]interface{}, shardCount/2)
	}
	return cm
}

// if you customize this map, you must define your own cmapHash<KeyType>

func (cm CMap) Shard(key string) *MapShard {
	h := cmapHashString(key)
	return &cm[h&uint64(len(cm)-1)]
}

func (cm CMap) Set(key string, val interface{})     { cm.Shard(key).Set(key, val) }
func (cm CMap) Get(key string) interface{}          { return cm.Shard(key).Get(key) }
func (cm CMap) Has(key string) bool                 { return cm.Shard(key).Has(key) }
func (cm CMap) Delete(key string)                   { cm.Shard(key).Delete(key) }
func (cm CMap) DeleteAndGet(key string) interface{} { return cm.Shard(key).DeleteAndGet(key) }

func (cm CMap) Foreach(fn ForeachFunc) {
	for i := range cm {
		cm[i].Foreach(fn)
	}
}

func (cm CMap) ForeachParallel(fn ForeachFunc) {
	var wg sync.WaitGroup
	wg.Add(len(cm))
	for i := range cm {
		go func(i int) {
			cm[i].Foreach(fn)
			wg.Done()
		}(i)
	}
	wg.Wait()
}

type KeyValue struct {
	Key   string
	Value interface{}
	Break bool
}

// Iter is an alias for IterBuffered(0)
func (cm CMap) Iter() <-chan *KeyValue {
	return cm.IterBuffered(0)
}

// IterBuffered returns a buffered channal shardCount * sz, to return an unbuffered channel you can pass 0
func (cm CMap) IterBuffered(sz int) <-chan *KeyValue {
	ch := make(chan *KeyValue, len(cm)*sz)
	go func() {
		var wg sync.WaitGroup
		wg.Add(len(cm))
		for i := range cm {
			go func(i int) {
				sh := &cm[i]
				sh.l.RLock()
				for k, v := range sh.m {
					kv := &KeyValue{k, v, false}
					ch <- kv
					if kv.Break {
						break
					}
				}
				sh.l.RUnlock()
				wg.Done()
			}(i)
		}
		wg.Wait()
		close(ch)
	}()
	return ch
}

func (cm CMap) iter(ch chan<- *KeyValue) {

}

func (cm CMap) Len() int {
	ln := 0
	for i := range cm {
		ln += cm[i].Len()
	}
	return ln
}