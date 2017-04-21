// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strconv"
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

func TestCalculateFeeAddresses(t *testing.T) {
	xpubStr := "tpubVpQL1h9UcY9c1BPZYfjYEtw5froRAvqZEo6sn5Tji6VkhcpfMaQ6id9Spf5iNvprRTcpdF5pj7m5Suyu1E8iC4xnb6MkjUnCJureTsmdXfG"
	firstAddrs := []string{
		"TsYLznZJn2xhM9F7Vnt7i39NuUFENGx9Hff",
		"TsiWMbdbmfMaJ9SDb7ig8EKfYp3KU3pvYfu",
		"TsgTraHPFWes88oTjpPVy7SEroJvgShv1G1",
	}
	params := &chaincfg.TestNet2Params

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
	expectedErr := fmt.Errorf("extended public key is for wrong network")
	addrs, err = calculateFeeAddresses(xpubStr, &chaincfg.MainNetParams)
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

var (
	c  *appContext
	wt WinningTicketsForBlock
)

func init() {

	c = &appContext{
		tickets: make(map[string]string),
		testing: true,
	}

	// Create a pool of tickets around expected size
	ticketCount := 49000
	userCount := 10000
	for i := 0; i < ticketCount; i++ {
		b := randomBytes(4)
		uid := int(binary.LittleEndian.Uint32(b)) % userCount
		msa := strconv.Itoa(uid | 1<<31)
		ticket := &chainhash.Hash{b[0], b[1], b[2], b[3]}

		// use ticket as the key
		c.tickets[ticket.String()] = msa

		// last 5 tickets win
		if i > ticketCount-6 {
			wt.winningTickets = append(wt.winningTickets, ticket)
		}
	}
}

func BenchmarkProcessWinningTickets(b *testing.B) {
	for n := 0; n < b.N; n++ {
		c.processWinningTickets(wt)
	}
}
