// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package dcrwallet provides methods that perform dcrwallet JSON-RPC procedure
// calls.
package dcrwallet

import (
	"context"
	"encoding/hex"

	"github.com/decred/base58"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	dcrdjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	walletjson "github.com/decred/dcrwallet/rpc/jsonrpc/types"
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

// RPC provides methods for calling dcrwallet JSON-RPCs without exposing the details
// of JSON encoding.
type RPC struct {
	Caller
}

// New creates a new RPC client instance from a caller.
func New(caller Caller) *RPC {
	return &RPC{caller}
}

// AccountSyncAddressIndex synchronizes an account branch to the passed address
// index.
func (r *RPC) AccountSyncAddressIndex(ctx context.Context, account string, branch uint32, index int) error {
	return r.Call(ctx, "accountsyncaddressindex", nil, account, branch, index)
}

// AddTicket manually adds a new ticket to the wallet stake manager. This is used
// to override normal security settings to insert tickets which would not
// otherwise be added to the wallet.
func (r *RPC) AddTicket(ctx context.Context, ticket *dcrutil.Tx) error {
	ticketB, err := ticket.MsgTx().Bytes()
	if err != nil {
		return err
	}
	return r.Call(ctx, "addticket", nil, hex.EncodeToString(ticketB))
}

// CreateMultisig creates a multisignature address that requires the specified
// number of signatures for the provided addresses and returns the
// multisignature address and script needed to redeem it.
func (r *RPC) CreateMultisig(ctx context.Context, requiredSigs int, addresses []dcrutil.Address) (*walletjson.CreateMultiSigResult, error) {
	addrs := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		addrs = append(addrs, addr.String())
	}
	res := &walletjson.CreateMultiSigResult{}
	if err := r.Call(ctx, "createmultisig", res, requiredSigs, addrs); err != nil {
		return nil, err
	}
	return res, nil
}

// DumpPrivKey gets the private key corresponding to the passed address
//
// NOTE: This function requires to the wallet to be unlocked.  See the
// WalletPassphrase function for more details.
func (r *RPC) DumpPrivKey(ctx context.Context, address dcrutil.Address) (*secp256k1.PrivateKey, error) {
	res := ""
	if err := r.Call(ctx, "dumpprivkey", &res, address.Address()); err != nil {
		return nil, err
	}
	decoded := base58.Decode(res)
	key := decoded[3 : len(decoded)-4]
	privKey, _ := secp256k1.PrivKeyFromBytes(key)
	return privKey, nil
}

// GenerateVote returns hex of an SSGen.
func (r *RPC) GenerateVote(ctx context.Context, blockHash *chainhash.Hash, height int64, sstxHash *chainhash.Hash, voteBits uint16, voteBitsExt string) (*walletjson.GenerateVoteResult, error) {
	res := &walletjson.GenerateVoteResult{}
	if err := r.Call(ctx, "generatevote", res, blockHash.String(), height, sstxHash.String(), voteBits, voteBitsExt); err != nil {
		return nil, err
	}
	return res, nil
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

// GetStakeInfo returns stake mining info from a given wallet. This includes
// various statistics on tickets it owns and votes it has produced.
func (r *RPC) GetStakeInfo(ctx context.Context) (*walletjson.GetStakeInfoResult, error) {
	res := &walletjson.GetStakeInfoResult{}
	if err := r.Call(ctx, "getstakeinfo", res); err != nil {
		return nil, err
	}
	return res, nil
}

// GetTickets returns a list of the tickets owned by the wallet, partially
// or in full. The flag includeImmature is used to indicate if non mature
// tickets should also be returned.
func (r *RPC) GetTickets(ctx context.Context, includeImmature bool) ([]*chainhash.Hash, error) {
	res := &walletjson.GetTicketsResult{}
	if err := r.Call(ctx, "gettickets", res, includeImmature); err != nil {
		return nil, err
	}
	tickets := make([]*chainhash.Hash, len(res.Hashes))
	for i := range res.Hashes {
		h, err := chainhash.NewHashFromStr(res.Hashes[i])
		if err != nil {
			return nil, err
		}
		tickets[i] = h
	}
	return tickets, nil
}

// GetTransaction returns detailed information about a wallet transaction.
func (r *RPC) GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error) {
	res := &walletjson.GetTransactionResult{}
	if err := r.Call(ctx, "gettransaction", res, txHash.String()); err != nil {
		return nil, err
	}
	return res, nil
}

// GetTransactionAsync returns a function that can be used to get a GetTransaction result at a later time.
func (r *RPC) GetTransactionAsync(ctx context.Context, txHash *chainhash.Hash) func() (*walletjson.GetTransactionResult, error) {
	gt := &walletjson.GetTransactionResult{}
	var err error
	wait := make(chan struct{})
	go func() {
		gt, err = r.GetTransaction(ctx, txHash)
		close(wait)
	}()
	return func() (*walletjson.GetTransactionResult, error) {
		<-wait
		return gt, err
	}
}

// ImportScriptRescanFrom attempts to import a byte code script into wallet. It
// also allows the user to choose whether or not they do a rescan, and which
// height to rescan from.
func (r *RPC) ImportScriptRescanFrom(ctx context.Context, script []byte, rescan bool, scanFrom int) error {
	return r.Call(ctx, "importscript", nil, hex.EncodeToString(script), rescan, scanFrom)
}

// ListScripts returns a list of the currently known redeemscripts from the
// wallet as a slice of byte slices.
func (r *RPC) ListScripts(ctx context.Context) ([][]byte, error) {
	res := &walletjson.ListScriptsResult{}
	if err := r.Call(ctx, "listscripts", res); err != nil {
		return nil, err
	}
	redeemScripts := make([][]byte, len(res.Scripts))
	for i := range res.Scripts {
		rs := res.Scripts[i].RedeemScript
		rsB, err := hex.DecodeString(rs)
		if err != nil {
			return nil, err
		}
		redeemScripts[i] = rsB
	}
	return redeemScripts, nil
}

// StakePoolUserInfo returns a list of tickets and information about them
// that are paying to the passed address.
func (r *RPC) StakePoolUserInfo(ctx context.Context, addr dcrutil.Address) (*walletjson.StakePoolUserInfoResult, error) {
	res := &walletjson.StakePoolUserInfoResult{}
	if err := r.Call(ctx, "stakepooluserinfo", res, addr.Address()); err != nil {
		return nil, err
	}
	return res, nil
}

// ValidateAddress returns information about the given Decred address.
func (r *RPC) ValidateAddress(ctx context.Context, addr dcrutil.Address) (*walletjson.ValidateAddressWalletResult, error) {
	res := &walletjson.ValidateAddressWalletResult{}
	if err := r.Call(ctx, "validateaddress", res, addr.Address()); err != nil {
		return nil, err
	}
	return res, nil
}

// Version returns information about the server's JSON-RPC API versions.
func (r *RPC) Version(ctx context.Context) (map[string]dcrdjson.VersionResult, error) {
	res := make(map[string]dcrdjson.VersionResult)
	if err := r.Call(ctx, "version", &res); err != nil {
		return nil, err
	}
	return res, nil
}

// WalletInfo returns wallet global state info for a given wallet.
func (r *RPC) WalletInfo(ctx context.Context) (*walletjson.WalletInfoResult, error) {
	res := &walletjson.WalletInfoResult{}
	if err := r.Call(ctx, "walletinfo", res); err != nil {
		return nil, err
	}
	return res, nil
}
