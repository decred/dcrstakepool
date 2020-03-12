// Copyright (c) 2019-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package system

import (
	"sync"
	"time"
)

type ticketChallengesCache struct {
	sync.RWMutex
	usedSignatures map[string]int64 // [signature]expiry
}

// newTicketChallengesCache creates a map to track ticket auth signatures
// that have been validated and used to prevent replay attacks.
// An infinite goroutine is used to 'untrack' signatures that have expired and
// can no longer be used regardless of whether or not they've been used before.
func newTicketChallengesCache() *ticketChallengesCache {
	cache := &ticketChallengesCache{
		usedSignatures: make(map[string]int64),
	}

	// goroutine to 'uncache' used signatures after they expire
	go func() {
		for now := range time.Tick(time.Second) {
			expiredSignatures := make([]string, 0)

			cache.RLock()
			for usedSignature, challengeExpiry := range cache.usedSignatures {
				if now.Unix() > challengeExpiry {
					expiredSignatures = append(expiredSignatures, usedSignature)
				}
			}
			cache.RUnlock()

			if len(expiredSignatures) > 0 {
				cache.Lock()
				for _, expiredSignature := range expiredSignatures {
					delete(cache.usedSignatures, expiredSignature)
				}
				cache.Unlock()
			}
		}
	}()

	return cache
}

func (cache *ticketChallengesCache) addChallenge(signature string, expiresIn int64) {
	cache.Lock()
	if _, ok := cache.usedSignatures[signature]; !ok {
		cache.usedSignatures[signature] = time.Now().Unix() + expiresIn
	}
	cache.Unlock()
}

func (cache *ticketChallengesCache) containsChallenge(signatures string) bool {
	cache.RLock()
	defer cache.RUnlock()
	_, exists := cache.usedSignatures[signatures]
	return exists
}
