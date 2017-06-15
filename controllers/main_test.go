package controllers

import (
	mrand "math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/decred/dcrd/chaincfg"
)

func TestGetNetworkName(t *testing.T) {
	// First test that "testnet2" is translated to "testnet"
	mc := MainController{
		params: &chaincfg.TestNet2Params,
	}

	netName := mc.getNetworkName()
	if netName != "testnet" {
		t.Errorf("Incorrect network name: expected %s, got %s", "testnet",
			netName)
	}

	// ensure "mainnet" is unaltered
	mc.params = &chaincfg.MainNetParams
	netName = mc.getNetworkName()
	if netName != "mainnet" {
		t.Errorf("Incorrect network name: expected %s, got %s", "mainnet",
			netName)
	}
}

func randHashString() string {
	var b [64]byte
	const hexvals = "123456789abcdef"
	for i := range b {
		b[i] = hexvals[mrand.Intn(len(hexvals))]
	}
	return string(b[:])
}

func TestSortByTicketHeight(t *testing.T) {
	// Create a large list of tickets to sort, voted over many blocks
	ticketCount, maxTxHeight := 55000, int64(123000)

	ticketInfoLive := make([]TicketInfoLive, 0, ticketCount)
	for i := 0; i < ticketCount; i++ {
		ticketInfoLive = append(ticketInfoLive, TicketInfoLive{
			TicketHeight: uint32(mrand.Int63n(maxTxHeight)),
			Ticket:       randHashString(), // could be nothing unless we sort with it
		})
	}

	// Make a copy to sort with ref method
	ticketInfoLive2 := make([]TicketInfoLive, len(ticketInfoLive))
	copy(ticketInfoLive2, ticketInfoLive)

	// Sort with ByTicketHeight, the test subject
	sort.Sort(ByTicketHeight(ticketInfoLive))

	// Sort using convenience function added in go1.8
	sort.Slice(ticketInfoLive2, func(i, j int) bool {
		return ticketInfoLive2[i].TicketHeight < ticketInfoLive2[j].TicketHeight
	})
	// compare
	if !reflect.DeepEqual(ticketInfoLive, ticketInfoLive2) {
		t.Error("Sort with ByTicketHeight failed")
	}

	// Check if sorted using convenience function added in go1.8
	if !sort.SliceIsSorted(ticketInfoLive, func(i, j int) bool {
		return ticketInfoLive[i].TicketHeight < ticketInfoLive[j].TicketHeight
	}) {
		t.Error("Sort with ByTicketHeight failed")
	}
}
