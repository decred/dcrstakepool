// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package main

import (
	"crypto/rand"
	"encoding/binary"
	mrand "math/rand"
	"strconv"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrstakepool/backend/stakepoold/stakepool"
	"github.com/decred/dcrstakepool/backend/stakepoold/userdata"
	"github.com/decred/dcrwallet/wallet/v3/udb"
)

func TestCalculateFeeAddresses(t *testing.T) {
	xpubStr := "tpubVpQL1h9UcY9c1BPZYfjYEtw5froRAvqZEo6sn5Tji6VkhcpfMaQ6id9Spf5iNvprRTcpdF5pj7m5Suyu1E8iC4xnb6MkjUnCJureTsmdXfG"
	firstAddrs := []string{
		"TsYLznZJn2xhM9F7Vnt7i39NuUFENGx9Hff",
		"TsiWMbdbmfMaJ9SDb7ig8EKfYp3KU3pvYfu",
		"TsgTraHPFWes88oTjpPVy7SEroJvgShv1G1",
	}
	params := chaincfg.TestNet3Params()

	// calculateFeeAddresses is currently hard-coded to return 10,000 addresses
	numAddr := 10000
	addrs, err := calculateFeeAddresses(xpubStr, params)
	if err != nil {
		t.Error("calculateFeeAddresses failed with ", err)
	}
	if len(addrs) != numAddr {
		t.Errorf("expected %d addresses, got %d", numAddr, len(addrs))
	}

	// Check that the first few addresses are in the map. NOTE: don't even think
	// about doing a range over the map as the order is random
	for _, addr := range firstAddrs {
		if _, ok := addrs[addr]; !ok {
			t.Errorf("Did not find address %s in derived address map", addr)
		}
	}

	// empty (i.e. invalid) xpubStr
	addrs, err = calculateFeeAddresses("", params)
	if err == nil {
		t.Error("calculateFeeAddresses did not error with empty extended key")
	}
	if len(addrs) != 0 {
		t.Errorf("expected empty map, actual length %d", len(addrs))
	}

	// wrong network
	expectedErr := hdkeychain.ErrWrongNetwork
	addrs, err = calculateFeeAddresses(xpubStr, chaincfg.MainNetParams())
	if err == nil {
		t.Error("calculateFeeAddresses did not error with wrong network parmas")
	}
	if err.Error() != expectedErr.Error() {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if len(addrs) != 0 {
		t.Errorf("expected empty map, actual length %d", len(addrs))
	}
}

func randomBytes(length int) []byte {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		panic(err.Error())
	}
	return b
}

func randString(n int) string {
	b := make([]byte, n)
	const addressLetters = "123456789abcdefghijkmnpQRSTUVWXYZ"
	for i := range b {
		b[i] = addressLetters[mrand.Intn(len(addressLetters))]
	}
	return string(b)
}

var (
	spd *stakepool.Stakepoold
	wt  stakepool.WinningTicketsForBlock
)

func init() {
	spd = &stakepool.Stakepoold{
		LiveTicketsMSA: make(map[chainhash.Hash]string),
		VotingConfig: &stakepool.VotingConfig{
			VoteBits:         1,
			VoteBitsExtended: "05000000",
			VoteVersion:      5,
		},
		UserVotingConfig: make(map[string]userdata.UserVotingConfig),
		Testing:          true,
	}

	// Create users
	userCount := 10000
	// leave out last 5, as they will be inserted when tickets are generated
	for i := 0; i < userCount-5; i++ {
		msa := "Tc" + randString(33)
		spd.UserVotingConfig[msa] = userdata.UserVotingConfig{
			Userid:          int64(i),
			MultiSigAddress: msa,
			VoteBits:        spd.VotingConfig.VoteBits,
			VoteBitsVersion: spd.VotingConfig.VoteVersion,
		}
	}

	// Create a pool of tickets around expected size
	ticketCount := 49000
	for i := 0; i < ticketCount; i++ {
		b := randomBytes(4)
		uid := int(binary.LittleEndian.Uint32(b)) % userCount
		msa := strconv.Itoa(uid | 1<<31)
		ticket := &chainhash.Hash{b[0], b[1], b[2], b[3]}

		// use ticket as the key
		spd.LiveTicketsMSA[*ticket] = msa

		// last 5 tickets win
		if i > ticketCount-6 {
			wt.WinningTickets = append(wt.WinningTickets, ticket)
			spd.UserVotingConfig[msa] = userdata.UserVotingConfig{}
		}
	}
}

func BenchmarkProcessWinningTickets(b *testing.B) {
	for n := 0; n < b.N; n++ {
		spd.ProcessWinningTickets(wt)
	}
}

var (
	xpubTestNet     = "tpubVpQL1h9UcY9c1BPZYfjYEtw5froRAvqZEo6sn5Tji6VkhcpfMaQ6id9Spf5iNvprRTcpdF5pj7m5Suyu1E8iC4xnb6MkjUnCJureTsmdXfG"
	xpubMainNet     = "dpubZGWjhGoJRkwao4W8Jsk56RPJNSAHmEtuERoeuugKmGxFhU1zhZJ2MfScJjzccGcs3xLHYZN2V7FjAHfBoiHdcpXtVyUnJQMxZxRENNgTEsM"
	xpubSimNet      = "spubVVBn1KgTWoDRajAZrymsoTRjP1qQdKTbuUMBBKw2q6vNVrbHXYGPTxDFgcaYYzrTRQ38mvkKt8dbk9pUHppT6WLZ23DroW8V3i3kptjfndx"
	childrenTestNet = []string{
		"TskTcbmvjYxduojxjAnTGxLArnGo4EFnoi3", "Tsg1JdVFUw9GVmW9dGo4ZCf3FGWw1FUaqvk",
		"TsbVWuTSuERse1yFeZHznF8xAHyyuSS8HkY", "Tso91gcUQdWZKkgLAeC8yWJ7D4AUE2vNiAC",
		"TsbZoFnCsHnsKV5xDSBtde8evQgfzrgrg1P", "Tsbxtn8e3T2yr42oV8psnigAJR7M58oKeZ2",
		"Tsf7dkLnsKHKzQEvenYGCgNozyDvnhQFtUu", "TsfDVfxHXPFBtNnwjywaa2gB1vwcnZmwHhR",
		"Tsft7d5AYU5mUwxDi6cJwr3SR8L2vExYcqe", "TsRhmeoBjYVfHqVjysChmXYcmo1VvX431qP"}
	childrenMainNet = []string{
		"Dshz7KtcGoPYbx3sW1Jh93TVi78VDGPb5te", "DsciYUd8QSunzAoqi5YJz7hmbZ7Gg7Y6Dhc",
		"DscxtcYoEDwiFmgV3ApdzG1Q7CZZcAhp4e8", "Dsg5sYFGyWj6w23rtSFCTnj1vCXZWh1vJTX",
		"DsnQ43inx8EmY8DxafFbNc8JP8vwKS1WfaV", "DsetJeaZRurv52TJkGQi2drYDCtyNS4Q9KR",
		"DsmyehuYCaHxUEJ44bLKF8r8AHU7c2Wv27A", "DsoKUmjgC61ZARQ3GVXREemAFVR1scnqnUL",
		"DsU31RL79PYHis8pxRLMj83AQaJzDtHE3TK", "DsS14B29Maf6Gq6hitLqLZyEcTZ5mUc5ne5"}
	childrenSimNet = []string{
		"SscWmiP9TMGZimomJiqQvnrkGe23h3C3sJb", "SsYn4toZtiSTbngZLxwvSFfUAh6RBpDVHJf",
		"SsaQHmC3GTbGJa4Djijh2mxPuAqp94RDTZX", "Ssi4NUey2gLWyfc78wikFbqu8sTXcdAs32A",
		"Ssf9mYdScWXpxrYBrttwKBhBpHaZ3iq5c9X", "Sso2bUfGA4sEFto1Ej7ka84jDZFjg7hNc64",
		"SsjyuWdpnaMWwzqYymLvEbEVgdpvZKyN9pg", "Ssi3nP3oZ8jD4G1WEgZuFtLmDo52kpGzuz6",
		"SsVwq3tCmRBHkDXy5mo2S2FB48wVbqguGx8", "SsqKRGQKCi6mm3YZkTyxSXXJWtCSSyXfPBG"}
)

type childAddressesTest struct {
	xpub     string
	net      *chaincfg.Params
	children []string
}

var childAddressesTests = []childAddressesTest{
	{xpubTestNet, chaincfg.TestNet3Params(), childrenTestNet},
	{xpubMainNet, chaincfg.MainNetParams(), childrenMainNet},
	{xpubSimNet, chaincfg.SimNetParams(), childrenSimNet},
}

func TestDeriveChildAddresses(t *testing.T) {
	for _, test := range childAddressesTests {
		key, err := hdkeychain.NewKeyFromString(test.xpub, test.net)
		if err != nil {
			t.Error(err)
			return
		}
		branchKey, err := key.Child(udb.ExternalBranch)
		if err != nil {
			t.Error(err)
			return
		}
		children, err := deriveChildAddresses(branchKey, 0, 10, test.net)
		if err != nil {
			t.Error(err)
			return
		}
		for i := range children {
			if children[i].Address() != test.children[i] {
				t.Errorf("for xpub %v on network %v at index %v expected child %v but got %v", test.xpub, test.net.Name, i, test.children[i], children[i].Address())
				return
			}
		}
	}
}
