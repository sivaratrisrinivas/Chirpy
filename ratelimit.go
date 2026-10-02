package main

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ipRateLimiter is a fixed-window limiter keyed by the TCP peer address.
// It deliberately ignores X-Forwarded-For, which clients can spoof.
type ipRateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*window
}

type window struct {
	start time.Time
	count int
}

func newIPRateLimiter(limit int, per time.Duration) *ipRateLimiter {
	return &ipRateLimiter{limit: limit, window: per, hits: map[string]*window{}}
}

func (l *ipRateLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[key] = &window{start: now, count: 1}
		if len(l.hits) > 10000 {
			for k, v := range l.hits {
				if now.Sub(v.start) >= l.window {
					delete(l.hits, k)
				}
			}
		}
		return true, 0
	}
	if w.count >= l.limit {
		return false, l.window - now.Sub(w.start)
	}
	w.count++
	return true, 0
}

func (l *ipRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ok, retry := l.allow(host, time.Now())
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
			respondWithError(w, http.StatusTooManyRequests, "Too many requests", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
