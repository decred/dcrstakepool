package system

import (
	"sync"
	"time"
)

type ticketChallengesCache struct {
	sync.Mutex
	usedSignatures map[string]int64 // [signature]expiry
}

func newTicketChallengesCache() *ticketChallengesCache {
	cache := &ticketChallengesCache{
		usedSignatures: make(map[string]int64, 0),
	}

	go func() {
		for now := range time.Tick(time.Second) {
			cache.Lock()
			for usedSignature, challengeExpiry := range cache.usedSignatures {
				if now.Unix() > challengeExpiry {
					delete(cache.usedSignatures, usedSignature)
				}
			}
			cache.Unlock()
		}
	}()

	return cache
}

func (cache *ticketChallengesCache) AddChallenge(signature string, expiresIn int64) {
	cache.Lock()
	if _, ok := cache.usedSignatures[signature]; !ok {
		cache.usedSignatures[signature] = time.Now().Unix() + expiresIn
	}
	cache.Unlock()
}

func (cache *ticketChallengesCache) ContainsChallenge(signatures string) (exists bool) {
	cache.Lock()
	_, exists = cache.usedSignatures[signatures]
	cache.Unlock()
	return
}
