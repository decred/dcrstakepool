package v3api

import (
	"sync"
	"time"
)

type ticketChallengesCache struct {
	sync.Mutex
	data map[string]int64  // [timestamp]expiry
}

func newTicketChallengesCache() *ticketChallengesCache {
	cache := &ticketChallengesCache{
		data: make(map[string]int64, 0),
	}

	go func() {
		for now := range time.Tick(time.Second) {
			cache.Lock()
			for timestampChallenge, challengeExpiry := range cache.data {
				if now.Unix() > challengeExpiry {
					delete(cache.data, timestampChallenge)
				}
			}
			cache.Unlock()
		}
	}()

	return cache
}

func (cache *ticketChallengesCache) addChallenge(timestamp string, expiresIn int64) {
	cache.Lock()
	if _, ok := cache.data[timestamp]; !ok {
		cache.data[timestamp] = time.Now().Unix() + expiresIn
	}
	cache.Unlock()
}

func (cache *ticketChallengesCache) containsChallenge(timestamp string) (exists bool) {
	cache.Lock()
	_, exists = cache.data[timestamp]
	cache.Unlock()
	return
}
