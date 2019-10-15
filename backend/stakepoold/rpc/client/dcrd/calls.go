// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package dcrd provides methods that perform dcrd JSON-RPC procedure
// calls.
package dcrd

import (
	"bytes"
	"context"
	"encoding/hex"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	dcrdjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/wire"
)

// Caller provides a client interface to perform JSON-RPC remote procedure calls.
type Caller interface {
	// Call performs the remote procedure call defined by method and
	// waits for a response or a broken client connection.
	// Args provides positional parameters for the call.
	// Res must be a pointer to a struct, slice, or map type to unmarshal
	// a result (if any), or nil if no result is needed.
	Call(ctx context.Context, method string, res interface{}, args ...interface{}) error
}

// RPC provides methods for calling dcrd JSON-RPCs without exposing the details
// of JSON encoding.
type RPC struct {
	Caller
}

// New creates a new RPC client instance from a caller.
func New(caller Caller) *RPC {
	return &RPC{Caller: caller}
}

// GetBestBlock returns the hash and height of the block in the longest (best)
// chain.
//
// NOTE: This is a dcrd extension.
func (r *RPC) GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error) {
	res := &dcrdjson.GetBestBlockResult{}
	if err := r.Call(ctx, "getbestblock", res); err != nil {
		return nil, 0, err
	}

	hash, err := chainhash.NewHashFromStr(res.Hash)
	if err != nil {
		return nil, 0, err
	}

	return hash, res.Height, nil
}

// GetBlockHeader returns the hash of the block in the best block chain at the
// given height.
func (r *RPC) GetBlockHeader(ctx context.Context, hash *chainhash.Hash) (*wire.BlockHeader, error) {
	res := ""
	if err := r.Call(ctx, "getblockheader", &res, hash.String(), false); err != nil {
		return nil, err
	}

	serializedBH, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}

	var bh wire.BlockHeader
	err = bh.Deserialize(bytes.NewReader(serializedBH))
	if err != nil {
		return nil, err
	}

	return &bh, nil
}

// GetConnectionCount returns the number of active connections to other peers.
func (r *RPC) GetConnectionCount(ctx context.Context) (int64, error) {
	var res int64
	if err := r.Call(ctx, "getconnectioncount", &res); err != nil {
		return 0, err
	}
	return res, nil
}

// GetCurrentNet returns the network the server is running on.
//
// NOTE: This is a dcrd extension.
func (r *RPC) GetCurrentNet(ctx context.Context) (wire.CurrencyNet, error) {
	var res int64
	if err := r.Call(ctx, "getcurrentnet", &res); err != nil {
		return 0, err
	}
	return wire.CurrencyNet(res), nil
}

// GetTransaction returns detailed information about a transaction.
func (r *RPC) GetRawTransaction(ctx context.Context, txHash *chainhash.Hash) (*dcrutil.Tx, error) {
	res := ""
	if err := r.Call(ctx, "getrawtransaction", &res, txHash.String()); err != nil {
		return nil, err
	}
	serializedTx, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}
	var msgTx wire.MsgTx
	if err := msgTx.Deserialize(bytes.NewReader(serializedTx)); err != nil {
		return nil, err
	}
	return dcrutil.NewTx(&msgTx), nil
}

// NotifyWinningTickets registers the client for winning ticket notifications.
func (r *RPC) NotifyWinningTickets(ctx context.Context) error {
	return r.Call(ctx, "notifywinningtickets", nil)
}

// NotifyNewTickets registers the client for new ticket notifications.
func (r *RPC) NotifyNewTickets(ctx context.Context) error {
	return r.Call(ctx, "notifynewtickets", nil)
}

// NotifySpentAndMissedTickets registers the client for spent and missed ticket
// notifications.
func (r *RPC) NotifySpentAndMissedTickets(ctx context.Context) error {
	return r.Call(ctx, "notifyspentandmissedtickets", nil)
}

// SendRawTransaction submits the encoded transaction to the server which will
// then relay it to the network.
func (r *RPC) SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	buf := bytes.NewBuffer(make([]byte, 0, tx.SerializeSize()))
	if err := tx.Serialize(buf); err != nil {
		return nil, err
	}
	res := ""
	if err := r.Call(ctx, "sendrawtransaction", &res, hex.EncodeToString(buf.Bytes()), allowHighFees); err != nil {
		return nil, err
	}
	hash, err := chainhash.NewHashFromStr(res)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

// Version returns information about the server's JSON-RPC API versions.
func (r *RPC) Version(ctx context.Context) (map[string]dcrdjson.VersionResult, error) {
	res := make(map[string]dcrdjson.VersionResult)
	if err := r.Call(ctx, "version", &res); err != nil {
		return nil, err
	}
	return res, nil
}
