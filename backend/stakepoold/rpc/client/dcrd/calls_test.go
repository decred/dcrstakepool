// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrd

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

// mockRPC holds the expected JSON query and response.
type mockRPC struct {
	res string
	req string
}

// Call satisfies the Caller interface.
func (m *mockRPC) Call(ctx context.Context, method string, res interface{}, args ...interface{}) error {
	// JSON format for RPC calls.
	request := struct {
		JSONRPC string        `json:"jsonrpc"`
		Method  string        `json:"method"`
		Params  []interface{} `json:"params,omitempty"`
		ID      uint32        `json:"id"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  args,
		ID:      0,
	}
	b, err := json.Marshal(&request)
	if err != nil {
		return err
	}
	// Test that the parsed request is the same as expected.
	if fmt.Sprintf("%s", b) != m.req {
		return fmt.Errorf("expected request %v does not match actual %v", m.req, request)
	}
	if res != nil {
		// Supply the response with the expected JSON data and ensure
		// it parses.
		if err := json.Unmarshal(json.RawMessage(m.res), res); err != nil {
			return err
		}
	}
	return nil
}

type getBestBlockTest struct {
	res    string
	req    string
	hash   string
	height int64
}

var getBestBlockTests = []getBestBlockTest{
	{
		req:    `{"jsonrpc":"2.0","method":"getbestblock","id":0}`,
		res:    `{"hash":"00000083b3316c655ffd195854739140a823f897e9478fe6d28f40766e05674f","height":287734}`,
		hash:   `00000083b3316c655ffd195854739140a823f897e9478fe6d28f40766e05674f`,
		height: 287734},
	{
		req:    `{"jsonrpc":"2.0","method":"getbestblock","id":0}`,
		res:    `{"hash":"000000000000000007018c6572e5e48a8370974d8b18f691240622d623b7c5a1","height":388973}`,
		hash:   `000000000000000007018c6572e5e48a8370974d8b18f691240622d623b7c5a1`,
		height: 388973},
}

func TestGetBestBlock(t *testing.T) {
	for _, test := range getBestBlockTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		bh, height, err := wallet.GetBestBlock(nil)
		if err != nil {
			t.Error(err)
			return
		}
		if bh.String() != test.hash {
			t.Errorf("expected hash %v does not match actual %v", test.hash, bh.String())
			return
		}
		if height != test.height {
			t.Errorf("expected block height %v does not match actual %v", test.height, height)
			return
		}
	}
}

type getBlockHeaderTest struct {
	res  string
	req  string
	hash string
}

var getBlockHeaderTests = []getBlockHeaderTest{
	{
		req:  `{"jsonrpc":"2.0","method":"getblockheader","params":["000000877d9a25b9e9eb6288870c986be119d04e8041235922b3520c4d70abe9",false],"id":0}`,
		res:  `"07000000068e112cb08131486acdb0993c65b9c960b4b09a788eb060f1c457db7900000000ae8bb94ac59e3976c2d4b64e40a98bf775b642754fd828ac4012afe7b8beec11cc34ecc46324303931fe52ffa6c8187fd42bfa7ad8d539cae633b397bd45e501008128da89a68d0500080068140000ffff001e5a934e8001000000086a0400053b0000dccaab5dee0c0a659d152dae0000000000000000000000000000000000000000000000000000000007000000"`,
		hash: `000000877d9a25b9e9eb6288870c986be119d04e8041235922b3520c4d70abe9`},
	{
		req:  `{"jsonrpc":"2.0","method":"getblockheader","params":["0000001a0169ca324f5bfd243243fbb1596f26d2940a8a742b0c6a6794d28065",false],"id":0}`,
		res:  `"0700000086c52a03be63b4b2e8fb363a9357028ba0475ff02fd5620553b98b8eee000000d23f105be54b1d42c00462e31180a121b7e538e1b2f46a3b62be9875a7d74a36cf201532a861bbd31feba80ab0974e785ee1e3a26e668f7681ac53c4ac56e9cf0100dd76da3a313705000e002c140000ffff001e1b578179020000008e6504005d25000058dfa85dcc03f9649d152dae0000000000000000000000000000000000000000000000000000000007000000"`,
		hash: `0000001a0169ca324f5bfd243243fbb1596f26d2940a8a742b0c6a6794d28065`},
}

func TestGetBlockHeader(t *testing.T) {
	for _, test := range getBlockHeaderTests {
		header, err := chainhash.NewHashFromStr(test.hash)
		if err != nil {
			t.Error(err)
			return
		}
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		bh, err := wallet.GetBlockHeader(nil, header)
		if err != nil {
			t.Error(err)
			return
		}
		if bh.BlockHash().String() != test.hash {
			t.Errorf("expected hash %v does not match actual %v", test.hash, bh.BlockHash().String())
			return
		}
	}
}

type getConnectionCountTest struct {
	res   string
	req   string
	count int64
}

var getConnectionCountTests = []getConnectionCountTest{
	{
		req:   `{"jsonrpc":"2.0","method":"getconnectioncount","id":0}`,
		res:   `5`,
		count: 5},
	{
		req:   `{"jsonrpc":"2.0","method":"getconnectioncount","id":0}`,
		res:   `10`,
		count: 10},
}

func TestGetConnectionCount(t *testing.T) {
	for _, test := range getConnectionCountTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		count, err := wallet.GetConnectionCount(nil)
		if err != nil {
			t.Error(err)
			return
		}
		if count != test.count {
			t.Errorf("expected count %v does not match actual %v", test.count, count)
			return
		}
	}
}

type getCurrentNetTest struct {
	res string
	req string
	net wire.CurrencyNet
}

var getCurrentNetTests = []getCurrentNetTest{
	{
		req: `{"jsonrpc":"2.0","method":"getcurrentnet","id":0}`,
		res: `2979310197`,
		net: wire.TestNet3},
	{
		req: `{"jsonrpc":"2.0","method":"getcurrentnet","id":0}`,
		res: `3652452601`,
		net: wire.MainNet},
}

func TestGetCurrentNet(t *testing.T) {
	for _, test := range getCurrentNetTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		net, err := wallet.GetCurrentNet(nil)
		if err != nil {
			t.Error(err)
			return
		}
		if net != test.net {
			t.Errorf("expected net %v does not match actual %v", test.net, net)
			return
		}
	}
}

type getTransactionTest struct {
	res  string
	req  string
	hash string
}

var getTransactionTests = []getTransactionTest{
	{
		req:  `{"jsonrpc":"2.0","method":"getrawtransaction","params":["e28bffa2d8c88bfc0aa5d2d36f5a0101148fe2b58a09c45bdf5fae0150f3436f"],"id":0}`,
		res:  `"01000000020000000000000000000000000000000000000000000000000000000000000000ffffffff00ffffffff42b17b4cd9d6a953c417dfc27795f5160fc49c258343e4c7d190db0c3b2cd87b0000000001ffffffff0400000000000000000000266a24a54dda04957b909292cf14ddc1313c95fa2e3a41a35910668df0b0020000000061ba030000000000000000000000086a06010007000000758903000000000000001abb76a914ec2ebb5e5313fac4faad85d26291dc615e4eb4b188acc45e4f880100000000001abb76a9140978d21009866c372316badeea80a00cd1e028ed88ac0000000000000000025b6fbc020000000000000000ffffffff020000df7896850100000095ad03000600000091483045022100a7fc82364e085e3c5670772abd749495b9723e6e5663bed414c98f8ec1483b56022005af772f86efb29cf9ddc8136cd4d75f58db06fb717368c8abdf23ae56dd139801475121032c4c0ec5caf4e2ae7df7d6d69757b549ba7c3c3f415d4768fb4e4ef27776cc2a2102325ee7f7b05557eee48d7663f3fe25a77f5343f22e9b0c70af65c70ba508114e52ae"`,
		hash: `e28bffa2d8c88bfc0aa5d2d36f5a0101148fe2b58a09c45bdf5fae0150f3436f`},
	{
		req:  `{"jsonrpc":"2.0","method":"getrawtransaction","params":["5072057ab5a8f4ea54de448d87f1c46da77fd63bd5b1ebf157d4b38e2630a643"],"id":0}`,
		res:  `"01000000020000000000000000000000000000000000000000000000000000000000000000ffffffff00ffffffff3de61810953e35f7a5b964f480149966adf4291a7c0746d74ff5701a035e24060000000001ffffffff0400000000000000000000266a24e86443a47039c2a1210504548bfb44a17ca9b492f5df08109c918bb3ba00000065ae030000000000000000000000086a06010007000000958903000000000000001abb76a914ec2ebb5e5313fac4faad85d26291dc615e4eb4b188accc725d880100000000001abb76a9148aea30c376345e7f5d32ab5a100be4f6bc9a915a88ac0000000000000000028383ca020000000000000000ffffffff020000df7896850100000095ad030008000000914830450221009bb01f5c43e691dd74d5e6e1a100127481bc3df85be0353269eb3ec247aa90d9022064b34c3d1c95c105e3477f67300225d31f725aec606b5ce73b4972c42e31c37501475121032c4c0ec5caf4e2ae7df7d6d69757b549ba7c3c3f415d4768fb4e4ef27776cc2a2102325ee7f7b05557eee48d7663f3fe25a77f5343f22e9b0c70af65c70ba508114e52ae"`,
		hash: `5072057ab5a8f4ea54de448d87f1c46da77fd63bd5b1ebf157d4b38e2630a643`},
}

func TestGetTransaction(t *testing.T) {
	for _, test := range getTransactionTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		hash, err := chainhash.NewHashFromStr(test.hash)
		if err != nil {
			t.Error(err)
			return
		}
		res, err := wallet.GetRawTransaction(nil, hash)
		if err != nil {
			t.Error(err)
			return
		}
		if res.Hash().String() != test.hash {
			t.Errorf("expected hash %v does not match actual %v", test.hash, res.Hash().String())
			return
		}
	}
}

type notifyWinningTicketsTest struct {
	res string
	req string
}

var notifyWinningTicketsTests = []notifyWinningTicketsTest{
	{
		req: `{"jsonrpc":"2.0","method":"notifywinningtickets","id":0}`,
		res: `null`},
}

func TestNotifyWinningTickets(t *testing.T) {
	for _, test := range notifyWinningTicketsTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		err := wallet.NotifyWinningTickets(nil)
		if err != nil {
			t.Error(err)
			return
		}
	}
}

type notifyNewTicketsTest struct {
	res string
	req string
}

var notifyNewTicketsTests = []notifyNewTicketsTest{
	{
		req: `{"jsonrpc":"2.0","method":"notifynewtickets","id":0}`,
		res: `null`},
}

func TestNotifyNewTickets(t *testing.T) {
	for _, test := range notifyNewTicketsTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		err := wallet.NotifyNewTickets(nil)
		if err != nil {
			t.Error(err)
			return
		}
	}
}

type notifySpentAndMissedTicketsTest struct {
	res string
	req string
}

var notifySpentAndMissedTicketsTests = []notifySpentAndMissedTicketsTest{
	{
		req: `{"jsonrpc":"2.0","method":"notifyspentandmissedtickets","id":0}`,
		res: `null`},
}

func TestNotifySpentAndMissedTickets(t *testing.T) {
	for _, test := range notifySpentAndMissedTicketsTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		err := wallet.NotifySpentAndMissedTickets(nil)
		if err != nil {
			t.Error(err)
			return
		}
	}
}

type versionTest struct {
	res string
	req string
	ver string
}

var versionTests = []versionTest{
	{
		req: `{"jsonrpc":"2.0","method":"version","id":0}`,
		res: `{"dcrd":{"versionstring":"1.5.0-pre+dev","major":1,"minor":5,"patch":0,"prerelease":"pre","buildmetadata":"dev.go1-13"},"dcrdjsonrpcapi":{"versionstring":"6.1.0","major":6,"minor":1,"patch":0,"prerelease":"","buildmetadata":""}}`,
		ver: `6.1.0`},
	{
		req: `{"jsonrpc":"2.0","method":"version","id":0}`,
		res: `{"dcrd":{"versionstring":"1.6.0-pre+dev","major":1,"minor":6,"patch":0,"prerelease":"pre","buildmetadata":"dev.go1-12-7"},"dcrdjsonrpcapi":{"versionstring":"6.1.1","major":6,"minor":1,"patch":1,"prerelease":"","buildmetadata":""}}`,
		ver: `6.1.1`},
}

func TestVersion(t *testing.T) {
	for _, test := range versionTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		ver, err := wallet.Version(nil)
		if err != nil {
			t.Error(err)
			return
		}
		v := ver["dcrdjsonrpcapi"]
		vStr := fmt.Sprintf("%v.%v.%v", v.Major, v.Minor, v.Patch)
		if vStr != test.ver {
			t.Errorf("expected version %v does not match actual %v", test.ver, vStr)
			return
		}
	}
}
