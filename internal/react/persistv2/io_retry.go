package persistv2

import (
	"errors"
	"math/rand"
	"os"
	"sync"
	"syscall"
	"time"
)

// P0 durability hardening:
// - We retry only idempotent writes (blobs + atomic snapshot writes).
// - We intentionally do NOT retry append-only events.jsonl writes, since failures
//   can be ambiguous (partial writes / fsync semantics) and blind retry can
//   duplicate events and corrupt state machines.

const (
	defaultIOMaxAttempts = 3
)

var (
	defaultIODelays = []time.Duration{
		50 * time.Millisecond,
		150 * time.Millisecond,
		450 * time.Millisecond,
	}
	ioRetrySleep = time.Sleep

	ioRetryRandMu sync.Mutex
	ioRetryRand   = rand.New(rand.NewSource(time.Now().UnixNano()))
)

func withIOWriteRetry(fn func() error) error {
	return withIOWriteRetryN(defaultIOMaxAttempts, fn)
}

func withIOWriteRetryN(maxAttempts int, fn func() error) error {
	if fn == nil {
		return nil
	}
	if maxAttempts <= 1 {
		return fn()
	}
	var last error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		last = fn()
		if last == nil {
			return nil
		}
		if !isTransientIOError(last) {
			return last
		}
		if attempt == maxAttempts {
			break
		}
		delay := defaultBackoffDelay(attempt)
		if delay > 0 && ioRetrySleep != nil {
			ioRetrySleep(delay)
		}
	}
	return last
}

func defaultBackoffDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	idx := attempt - 1
	base := time.Duration(0)
	if idx >= 0 && idx < len(defaultIODelays) {
		base = defaultIODelays[idx]
	} else if len(defaultIODelays) > 0 {
		base = defaultIODelays[len(defaultIODelays)-1]
	}
	if base <= 0 {
		return 0
	}
	// Small jitter to avoid retry synchronization.
	jitter := base / 5 // 20%
	if jitter <= 0 {
		return base
	}
	ioRetryRandMu.Lock()
	delta := time.Duration(ioRetryRand.Int63n(int64(jitter*2+1))) - jitter
	ioRetryRandMu.Unlock()
	return base + delta
}

func isTransientIOError(err error) bool {
	if err == nil {
		return false
	}
	// Explicitly do not retry permission / existence / invalid input errors.
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
		return false
	}
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EROFS) {
		return false
	}
	// Retry the obvious transient syscalls.
	if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
		return true
	}
	// Some callers wrap in *os.PathError / *os.SyscallError; errors.Is handles that.
	if os.IsTimeout(err) {
		return true
	}
	return false
}
