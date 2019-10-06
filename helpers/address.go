// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package helpers

import (
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
)

// DCRutilAddressFromExtendedKey returns a dcrutil Address object for extended
// key and params.
func DCRutilAddressFromExtendedKey(key *hdkeychain.ExtendedKey, params *chaincfg.Params) (*dcrutil.AddressPubKeyHash, error) {
	ecPubKey, err := key.ECPubKey()
	if err != nil {
		return nil, err
	}
	return dcrutil.NewAddressPubKeyHash(dcrutil.Hash160(ecPubKey.Serialize()), params, dcrec.STEcdsaSecp256k1)
}
