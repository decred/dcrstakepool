// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package helpers

import (
	"testing"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrwallet/wallet/v3/udb"
)

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

type dcrutilAddressTest struct {
	xpub     string
	net      *chaincfg.Params
	children []string
}

var dcrutilAddressTests = []dcrutilAddressTest{
	{xpubTestNet, chaincfg.TestNet3Params(), childrenTestNet},
	{xpubMainNet, chaincfg.MainNetParams(), childrenMainNet},
	{xpubSimNet, chaincfg.SimNetParams(), childrenSimNet},
}

func TestDCRUtilAddressFromExtendedKey(t *testing.T) {
	for _, test := range dcrutilAddressTests {
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
		for i := 0; i < 10; i++ {
			child, err := branchKey.Child(uint32(i))
			if err == hdkeychain.ErrInvalidChild {
				continue
			}
			if err != nil {
				t.Error(err)
				return
			}
			addr, err := DCRUtilAddressFromExtendedKey(child, test.net)
			if err != nil {
				t.Error(err)
				return
			}
			if addr.Address() != test.children[i] {
				t.Errorf("for xpub %v on network %v at index %v expected address %v but got %v", test.xpub, test.net.Name, i, test.children[i], addr.Address())
				return
			}
		}
	}
}
