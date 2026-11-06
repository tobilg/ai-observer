package handlers

import (
	"container/list"
	"context"
	"crypto/sha256"
	"hash"
	"sync"
	"time"
)

// Exporters retry an export when they get no response, also when the server
// stored it and only the response was lost. Spans are deduplicated by their
// IDs when stored, but logs and metric points have no identity, so a retried
// batch would be stored twice: delta counters such as Claude Code's cost would
// count it twice. batchDedup remembers the request bodies stored recently and
// lets a byte-identical retry succeed without storing it again.
//
// The set lives in memory: a retry that arrives after a restart is stored
// again. Two different exports never have identical bodies, because their
// records carry timestamps.

const (
	dedupTTL        = 15 * time.Minute // longer than exporters keep retrying
	dedupMaxEntries = 10_000
)

type batchKey [sha256.Size]byte

// batchHasher hashes a request body as it is read, together with the signal
// it was sent for
type batchHasher struct {
	hash.Hash
}

func newBatchHasher(signal string) batchHasher {
	h := sha256.New()
	h.Write([]byte(signal))
	h.Write([]byte{0})
	return batchHasher{h}
}

func (h batchHasher) key() batchKey {
	var key batchKey
	h.Sum(key[:0])
	return key
}

type batchDedup struct {
	mu      sync.Mutex
	entries map[batchKey]*dedupEntry
	stored  *list.List // keys of stored batches, oldest first
	ttl     time.Duration
	max     int
	now     func() time.Time
}

type dedupEntry struct {
	done    chan struct{} // closed when the request storing the batch has finished
	stored  bool
	expires time.Time
	elem    *list.Element // in batchDedup.stored, once stored
}

func newBatchDedup() *batchDedup {
	return &batchDedup{
		entries: make(map[batchKey]*dedupEntry),
		stored:  list.New(),
		ttl:     dedupTTL,
		max:     dedupMaxEntries,
		now:     time.Now,
	}
}

// claim reports whether the batch with key must be stored. It returns false
// for a batch that was stored recently. While another request is storing the
// same batch, claim waits for its outcome. After claim returned true, the
// caller must call release with whether the batch was stored.
func (d *batchDedup) claim(ctx context.Context, key batchKey) (bool, error) {
	for {
		d.mu.Lock()
		d.expire()
		e, ok := d.entries[key]
		if !ok {
			d.entries[key] = &dedupEntry{done: make(chan struct{})}
			d.mu.Unlock()
			return true, nil
		}
		if e.stored {
			d.mu.Unlock()
			return false, nil
		}
		done := e.done
		d.mu.Unlock()

		select {
		case <-done:
			// Stored, or failed and released: look again
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

// release ends a claim. A stored batch is remembered for the TTL; a batch that
// could not be stored is forgotten, so the exporter's retry stores it.
func (d *batchDedup) release(key batchKey, stored bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.entries[key]
	if !ok || e.stored {
		return
	}
	if stored {
		e.stored = true
		e.expires = d.now().Add(d.ttl)
		e.elem = d.stored.PushBack(key)
		for d.stored.Len() > d.max {
			d.forget(d.stored.Front())
		}
	} else {
		delete(d.entries, key)
	}
	close(e.done)
}

// expire forgets the stored batches whose TTL has passed. d.mu must be held.
func (d *batchDedup) expire() {
	now := d.now()
	for front := d.stored.Front(); front != nil; front = d.stored.Front() {
		if d.entries[front.Value.(batchKey)].expires.After(now) {
			return
		}
		d.forget(front)
	}
}

// forget removes a stored batch. d.mu must be held.
func (d *batchDedup) forget(elem *list.Element) {
	delete(d.entries, elem.Value.(batchKey))
	d.stored.Remove(elem)
}
