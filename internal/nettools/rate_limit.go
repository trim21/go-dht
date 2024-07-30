package nettools

import (
	"time"

	"github.com/hashicorp/golang-lru/v2"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

func NewThrottler(maxPerMinute int, maxHosts int64) *ClientThrottle {
	r := ClientThrottle{
		maxPerMinute: maxPerMinute,
		c:            must(lru.New[string, hits](int(maxHosts))),
		blocked:      must(lru.New[string, hits](int(maxHosts))),
		stop:         make(chan bool),
	}
	go r.cleanup()
	return &r
}

// ClientThrottle identifies and blocks hosts that are too spammy. It only
// cares about the number of operations per minute.
type ClientThrottle struct {
	maxPerMinute int

	// Rate limiter.
	c *lru.Cache[string, hits]

	// Hosts that get blocked once go to a separate cache, and stay forever
	// until they stop hitting us enough to fall off the blocked cache.
	blocked *lru.Cache[string, hits]

	// This channel will be closed when the ClientThrottle should be stopped.
	stop chan bool
}

// Stop the ClientThrottle and all internal goroutines.
func (r *ClientThrottle) Stop() {
	close(r.stop)

}

func (r *ClientThrottle) CheckBlock(host string) bool {
	_, blocked := r.blocked.Get(host)
	if blocked {
		// Bad guy stays there.
		return false
	}

	v, ok := r.c.Get(host)
	var h hits
	if !ok {
		h = hits(59)
	} else {
		h = v - 1
	}
	if int(h) < 60-r.maxPerMinute {
		// fmt.Printf("blocking because int(h)=%v < 60-r.maxPerMinute = (60-%v) => %v\n", int(h), r.maxPerMinute, 60-r.maxPerMinute)
		r.c.Add(host, h-300)
		// New bad guy.
		r.blocked.Add(host, h) // The value here is not relevant.
		return false
	}
	r.c.Add(host, h)
	return true
}

// refill the buckets.
// this is the first way I thought of how to implement client rate limiting.
// Need to think and research more.
func (r *ClientThrottle) cleanup() {
	// Check the bucket faster than the rate period, to reduce the pressure in the cache.
	t := time.Tick(5 * time.Second)

	for {
		select {
		case <-t:
			// This is ridiculously inefficient but it'll have to do for now.
			for _, key := range r.c.Keys() {
				item, ok := r.c.Get(key)
				if !ok {
					continue
				}

				h := item + 5
				if h > 60 {
					// Reduce pressure in the LRU.
					r.c.Remove(key)
				} else {
					r.c.Add(key, h)
				}
			}
		case <-r.stop:
			return
		}
	}
}

type hits int
