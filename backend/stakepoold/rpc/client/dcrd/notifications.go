package dcrd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

// wrongNumParams is an error type describing an unparseable JSON-RPC
// notification due to an incorrect number of parameters for the
// expected notification type.  The value is the number of parameters
// of the invalid notification.
type wrongNumParams int

// Error satisfies the builtin error interface.
func (e wrongNumParams) Error() string {
	return fmt.Sprintf("wrong number of parameters (%d)", e)
}

// parseNewTicketsNtfnParams parses out the block header hash, height,
// winner number, overflow, and ticket map from a NewTickets notification.
func ParseNewTickets(param json.RawMessage) (*chainhash.Hash, int64, int64, []*chainhash.Hash, error) {

	params := []json.RawMessage{}
	json.Unmarshal(param, &params)

	if len(params) != 4 {
		return nil, 0, 0, nil, wrongNumParams(len(params))
	}

	// Unmarshal first parameter as a string.
	var blockShaStr string
	err := json.Unmarshal(params[0], &blockShaStr)
	if err != nil {
		return nil, 0, 0, nil, err
	}

	// Create ShaHash from block sha string.
	sha, err := chainhash.NewHashFromStr(blockShaStr)
	if err != nil {
		return nil, 0, 0, nil, err
	}

	// Unmarshal second parameter as an integer.
	var blockHeight int32
	err = json.Unmarshal(params[1], &blockHeight)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	bh := int64(blockHeight)

	// Unmarshal third parameter as an integer.
	var stakeDiff int64
	err = json.Unmarshal(params[2], &stakeDiff)
	if err != nil {
		return nil, 0, 0, nil, err
	}

	// Unmarshal fourth parameter as a slice.
	var tickets []string
	err = json.Unmarshal(params[3], &tickets)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	t := make([]*chainhash.Hash, len(tickets))

	for i, ticketHashStr := range tickets {
		ticketHash, err := chainhash.NewHashFromStr(ticketHashStr)
		if err != nil {
			return nil, 0, 0, nil, err
		}

		t[i] = ticketHash
	}

	return sha, bh, stakeDiff, t, nil
}

// parseSpentAndMissedTicketsNtfnParams parses out the block header hash, height,
// winner number, and ticket map from a SpentAndMissedTickets notification.
func ParseSpentAndMissedTickets(param json.RawMessage) (
	*chainhash.Hash,
	int64,
	int64,
	map[chainhash.Hash]bool,
	error) {

	params := []json.RawMessage{}
	json.Unmarshal(param, &params)

	if len(params) != 4 {
		return nil, 0, 0, nil, wrongNumParams(len(params))
	}

	// Unmarshal first parameter as a string.
	var blockShaStr string
	err := json.Unmarshal(params[0], &blockShaStr)
	if err != nil {
		return nil, 0, 0, nil, err
	}

	// Create ShaHash from block sha string.
	sha, err := chainhash.NewHashFromStr(blockShaStr)
	if err != nil {
		return nil, 0, 0, nil, err
	}

	// Unmarshal second parameter as an integer.
	var blockHeight int32
	err = json.Unmarshal(params[1], &blockHeight)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	bh := int64(blockHeight)

	// Unmarshal third parameter as an integer.
	var stakeDiff int64
	err = json.Unmarshal(params[2], &stakeDiff)
	if err != nil {
		return nil, 0, 0, nil, err
	}

	// Unmarshal fourth parameter as a map[*hash]bool.
	tickets := make(map[string]string)
	err = json.Unmarshal(params[3], &tickets)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	t := make(map[chainhash.Hash]bool)

	for hashStr, spentStr := range tickets {
		isSpent := false
		if spentStr == "spent" {
			isSpent = true
		}

		// Create and cache ShaHash from tx hash.
		ticketSha, err := chainhash.NewHashFromStr(hashStr)
		if err != nil {
			return nil, 0, 0, nil, err
		}

		t[*ticketSha] = isSpent
	}

	return sha, bh, stakeDiff, t, nil
}

// parseWinningTicketsNtfnParams parses out the list of eligible tickets, block
// hash, and block height from a WinningTickets notification.
func ParseWinningTickets(param json.RawMessage) (
	*chainhash.Hash,
	int64,
	[]*chainhash.Hash,
	error) {

	params := []json.RawMessage{}
	json.Unmarshal(param, &params)

	if len(params) != 3 {
		return nil, 0, nil, wrongNumParams(len(params))
	}

	// Unmarshal first parameter as a string.
	var blockHashStr string
	err := json.Unmarshal(params[0], &blockHashStr)
	if err != nil {
		return nil, 0, nil, err
	}

	// Create ShaHash from block sha string.
	bHash, err := chainhash.NewHashFromStr(blockHashStr)
	if err != nil {
		return nil, 0, nil, err
	}

	// Unmarshal second parameter as an integer.
	var blockHeight int32
	err = json.Unmarshal(params[1], &blockHeight)
	if err != nil {
		return nil, 0, nil, err
	}
	bHeight := int64(blockHeight)

	// Unmarshal third parameter as a slice.
	tickets := make(map[string]string)
	err = json.Unmarshal(params[2], &tickets)
	if err != nil {
		return nil, 0, nil, err
	}
	t := make([]*chainhash.Hash, len(tickets))

	for i, ticketHashStr := range tickets {
		// Create and cache Hash from tx hash.
		ticketHash, err := chainhash.NewHashFromStr(ticketHashStr)
		if err != nil {
			return nil, 0, nil, err
		}

		itr, err := strconv.Atoi(i)
		if err != nil {
			return nil, 0, nil, err
		}

		t[itr] = ticketHash
	}

	return bHash, bHeight, t, nil
}
