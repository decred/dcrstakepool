// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/hdkeychain"
)

const (
	//real pubkeys
	testnetXPub1 = "tpubVpFtCRJV1U7fJQYLiDtKTZFnwwaHp6uhcavTjvXBusiY1pDQw5YqT1HGDcQT2wjLQZnL7N66o7atgscq6tMP6fr5ejyqDHD3eQ9C3KQURzu"
	testnetXPub2 = "tpubVpFtCRJV1U7fMienqUufobnNxxYACoLDTaAFmpspHy2iguBzoGRbi4btArDijbsNVvMVnciEC7ZHMCr8T19Ln7ECBuAT5UqYW21cKcNxMN6"
	mainnetXPub1 = "dpubZGWjhGoJRkwao4W8Jsk56RPJNSAHmEtuERoeuugKmGxFhU1zhZJ2MfScJjzccGcs3xLHYZN2V7FjAHfBoiHdcpXtVyUnJQMxZxRENNgTEsM"
	mainnetXPub2 = "dpubZGWjhGoJRkwaqnFhB6a85gjvSSbLuDxAj7ixmFqcu7RZPW7cthZU4jK7u7LZSrr7ooHGHFc1LEn1n9cypSvzKknqZYugKda6PYc5Ze7NhTL"
	simnetXPub1  = "spubVVBn1KgTWoDRajAZrymsoTRjP1qQdKTbuUMBBKw2q6vNVrbHXYGPTxDFgcaYYzrTRQ38mvkKt8dbk9pUHppT6WLZ23DroW8V3i3kptjfndx"
	simnetXPub2  = "spubVVBn1KgTWoDRefX2cjSRjhBYFahdbTvhMzB1Lia3hCseDjB4tdxFJ3FDPG3NGkBpA6XEjRxw1r9LnU5nRpkvKGkfxfAqqFtc72kaU5Fmn6r"
)

type keyParse struct {
	params     *chaincfg.Params
	coldFeeIn  string
	coldFeeOut *hdkeychain.ExtendedKey
	isError    bool
}

//in keys, expected out keys, and error value
var keyTestValues = []keyParse{
	//testnet
	{testNet3Params.Params, testnetXPub1, hd(testnetXPub1), false},
	{testNet3Params.Params, mainnetXPub2, hd(mainnetXPub2), true},
	{testNet3Params.Params, "", hd(""), true},
	//mainnet
	{mainNetParams.Params, mainnetXPub1, hd(mainnetXPub1), false},
	{mainNetParams.Params, simnetXPub2, hd(simnetXPub2), true},
	{mainNetParams.Params, mainnetXPub2 + "a", hd(mainnetXPub2 + "a"), true},
	//simnnet
	{simNetParams.Params, simnetXPub1, hd(simnetXPub1), false},
	{simNetParams.Params, testnetXPub2, hd(testnetXPub2), true},
	{simNetParams.Params, simnetXPub1[:len(simnetXPub1)-1], hd(simnetXPub1[:len(simnetXPub1)-1]), true},
}

//helper func string to extended key
func hd(s string) *hdkeychain.ExtendedKey {
	hd, _ := hdkeychain.NewKeyFromString(s)
	return hd
}

//an error will produce a nil key struct so use nil string value
func strFromHd(hd *hdkeychain.ExtendedKey) string {
	if hd == nil {
		return ""
	}
	return hd.String()
}

func TestParsePubKeys(t *testing.T) {
	//dummy config
	var cfg config
	for _, test := range keyTestValues {
		//parsePubKeys uses these fields
		cfg.ColdWalletExtPub = test.coldFeeIn
		//testing func
		err := cfg.parsePubKeys(test.params)
		//err if expected output key strings and real output key strings don't match or expected error status is different
		if strFromHd(test.coldFeeOut) != strFromHd(coldWalletFeeKey) || (err != nil) != test.isError {
			t.Error("for", test.coldFeeIn, "expected", strFromHd(test.coldFeeOut), "and is error=", test.isError, "got", strFromHd(coldWalletFeeKey), "and is error=", err != nil)
		}
	}
}
