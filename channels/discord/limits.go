package discord

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	permit chan struct{}
	reset  time.Time
}

func newRateBucket() *rateBucket {
	return &rateBucket{permit: make(chan struct{}, 1)}
}

func (b *rateBucket) lock(ctx context.Context) error {
	select {
	case b.permit <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *rateBucket) unlock() {
	<-b.permit
}

type rateLimiter struct {
	mu      sync.Mutex
	routes  map[string]*rateBucket
	buckets map[string]*rateBucket
	global  time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		routes:  make(map[string]*rateBucket),
		buckets: make(map[string]*rateBucket),
	}
}

func (l *rateLimiter) lock(ctx context.Context, route string) (*rateBucket, error) {
	for {
		l.mu.Lock()
		bucket := l.routes[route]
		if bucket == nil {
			bucket = newRateBucket()
			l.routes[route] = bucket
		}
		global := l.global
		l.mu.Unlock()

		if err := waitUntil(ctx, global); err != nil {
			return nil, err
		}
		if err := bucket.lock(ctx); err != nil {
			return nil, err
		}
		l.mu.Lock()
		current := l.routes[route]
		reset := bucket.reset
		global = l.global
		l.mu.Unlock()
		if current != bucket {
			bucket.unlock()
			continue
		}
		if err := waitUntil(ctx, reset); err != nil {
			bucket.unlock()
			return nil, err
		}
		if err := waitUntil(ctx, global); err != nil {
			bucket.unlock()
			return nil, err
		}
		return bucket, nil
	}
}

func (l *rateLimiter) observe(route string, current *rateBucket, header http.Header) {
	l.mu.Lock()
	defer l.mu.Unlock()
	bucketID := header.Get("X-RateLimit-Bucket")
	target := current
	if bucketID != "" {
		major := route
		if index := strings.LastIndexByte(route, ' '); index >= 0 {
			major = route[index+1:]
		}
		bucketKey := bucketID + " " + major
		known := l.buckets[bucketKey]
		if known == nil {
			l.buckets[bucketKey] = current
		} else {
			l.routes[route] = known
			target = known
		}
	}
	remaining, err := strconv.Atoi(header.Get("X-RateLimit-Remaining"))
	if err != nil || remaining != 0 {
		return
	}
	if duration, ok := secondsHeader(header.Get("X-RateLimit-Reset-After")); ok {
		deadline := time.Now().Add(duration)
		if deadline.After(current.reset) {
			current.reset = deadline
		}
		if deadline.After(target.reset) {
			target.reset = deadline
		}
	}
}

func (l *rateLimiter) limited(bucket *rateBucket, retry time.Duration, global bool) {
	deadline := time.Now().Add(retry)
	if global {
		l.mu.Lock()
		if deadline.After(l.global) {
			l.global = deadline
		}
		l.mu.Unlock()
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if deadline.After(bucket.reset) {
		bucket.reset = deadline
	}
}

func secondsHeader(value string) (time.Duration, bool) {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

func waitUntil(ctx context.Context, deadline time.Time) error {
	delay := time.Until(deadline)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
