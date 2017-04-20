package main

import (
	"fmt"
	"testing"

	"github.com/decred/dcrd/chaincfg"
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
