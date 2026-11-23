package identity

import (
	"crypto/sha256"
	"sync"
	"time"
)

type limiter struct {
	mu       sync.Mutex
	start    time.Time
	total    int
	attempts map[[32]byte]int
	work     chan struct{}
}

func newLimiter() *limiter {
	return &limiter{work: make(chan struct{}, 4)}
}

func (l *limiter) admit(action, username string) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if l.start.IsZero() || now.Sub(l.start) >= time.Minute {
		l.start, l.total = now, 0
		l.attempts = make(map[[32]byte]int)
	}

	key := sha256.Sum256([]byte(action + ":" + username))
	if l.total >= 120 || l.attempts[key] >= 8 {
		return nil, false
	}
	select {
	case l.work <- struct{}{}:
		l.total++
		l.attempts[key]++
		return func() { <-l.work }, true
	default:
		return nil, false
	}
}

func (l *limiter) forgive(action, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, sha256.Sum256([]byte(action+":"+username)))
}
