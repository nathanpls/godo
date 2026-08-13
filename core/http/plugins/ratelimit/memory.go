package ratelimit

import (
	"container/heap"
	"context"
	"errors"
	"sync"
	"time"
)

const defaultMaxMemoryKeys = 100_000

var errMemoryStoreFull = errors.New("ratelimit: memory store key limit reached")

type memoryBucket struct {
	count int64
	reset time.Time
}

type expiration struct {
	key   string
	reset time.Time
}

type expirationHeap []expiration

func (entries expirationHeap) Len() int           { return len(entries) }
func (entries expirationHeap) Less(i, j int) bool { return entries[i].reset.Before(entries[j].reset) }
func (entries expirationHeap) Swap(i, j int)      { entries[i], entries[j] = entries[j], entries[i] }
func (entries *expirationHeap) Push(value any)    { *entries = append(*entries, value.(expiration)) }
func (entries *expirationHeap) Pop() any {
	old := *entries
	last := old[len(old)-1]
	*entries = old[:len(old)-1]
	return last
}

// MemoryStore keeps limits in the current process. It is safe for concurrent
// use but does not coordinate limits across processes.
type MemoryStore struct {
	mu          sync.Mutex
	buckets     map[string]memoryBucket
	expirations expirationHeap
	maxKeys     int
}

// NewMemoryStore creates an empty in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{buckets: make(map[string]memoryBucket), maxKeys: defaultMaxMemoryKeys}
}

// Take atomically consumes one request from key's current window.
func (store *MemoryStore) Take(ctx context.Context, key string, limit int64, window time.Duration, now time.Time) (Result, error) {
	if store == nil {
		return Result{}, errors.New("ratelimit: memory store must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.buckets == nil {
		store.buckets = make(map[string]memoryBucket)
	}
	if store.maxKeys == 0 {
		store.maxKeys = defaultMaxMemoryKeys
	}
	for store.expirations.Len() > 0 && !now.Before(store.expirations[0].reset) {
		expired := heap.Pop(&store.expirations).(expiration)
		if bucket, exists := store.buckets[expired.key]; exists && bucket.reset.Equal(expired.reset) {
			delete(store.buckets, expired.key)
		}
	}

	bucket, exists := store.buckets[key]
	if !exists || !now.Before(bucket.reset) {
		if !exists && len(store.buckets) >= store.maxKeys {
			return Result{}, errMemoryStoreFull
		}
		bucket = memoryBucket{reset: now.Add(window)}
		heap.Push(&store.expirations, expiration{key: key, reset: bucket.reset})
	}
	allowed := bucket.count < limit
	if allowed {
		bucket.count++
	}
	store.buckets[key] = bucket
	return Result{Allowed: allowed, Remaining: max(0, limit-bucket.count), Reset: bucket.reset}, nil
}
