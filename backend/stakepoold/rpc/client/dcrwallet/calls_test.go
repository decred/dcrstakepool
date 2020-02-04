// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrwallet

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
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

type accountSyncAddressIndexTest struct {
	res     string
	req     string
	account string
	branch  uint32
	index   int
}

var accountSyncAddressIndexTests = []accountSyncAddressIndexTest{
	{
		req:     `{"jsonrpc":"2.0","method":"accountsyncaddressindex","params":["default",1,7],"id":0}`,
		res:     `null`,
		account: "default",
		branch:  1,
		index:   7},
}

func TestAccountSyncAddressIndex(t *testing.T) {
	for _, test := range accountSyncAddressIndexTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		err := wallet.AccountSyncAddressIndex(nil, test.account, test.branch, test.index)
		if err != nil {
			t.Error(err)
			return
		}
	}
}

type addTicketTest struct {
	res     string
	req     string
	wireMsg string
}

var addTicketTests = []addTicketTest{
	{
		req:     `{"jsonrpc":"2.0","method":"addticket","params":["01000000020000000000000000000000000000000000000000000000000000000000000000ffffffff00ffffffff3de61810953e35f7a5b964f480149966adf4291a7c0746d74ff5701a035e24060000000001ffffffff0400000000000000000000266a24e86443a47039c2a1210504548bfb44a17ca9b492f5df08109c918bb3ba00000065ae030000000000000000000000086a06010007000000958903000000000000001abb76a914ec2ebb5e5313fac4faad85d26291dc615e4eb4b188accc725d880100000000001abb76a9148aea30c376345e7f5d32ab5a100be4f6bc9a915a88ac0000000000000000028383ca020000000000000000ffffffff020000df7896850100000000000000ffffffff914830450221009bb01f5c43e691dd74d5e6e1a100127481bc3df85be0353269eb3ec247aa90d9022064b34c3d1c95c105e3477f67300225d31f725aec606b5ce73b4972c42e31c37501475121032c4c0ec5caf4e2ae7df7d6d69757b549ba7c3c3f415d4768fb4e4ef27776cc2a2102325ee7f7b05557eee48d7663f3fe25a77f5343f22e9b0c70af65c70ba508114e52ae"],"id":0}`,
		res:     `null`,
		wireMsg: `01000000020000000000000000000000000000000000000000000000000000000000000000ffffffff00ffffffff3de61810953e35f7a5b964f480149966adf4291a7c0746d74ff5701a035e24060000000001ffffffff0400000000000000000000266a24e86443a47039c2a1210504548bfb44a17ca9b492f5df08109c918bb3ba00000065ae030000000000000000000000086a06010007000000958903000000000000001abb76a914ec2ebb5e5313fac4faad85d26291dc615e4eb4b188accc725d880100000000001abb76a9148aea30c376345e7f5d32ab5a100be4f6bc9a915a88ac0000000000000000028383ca020000000000000000ffffffff020000df7896850100000000000000ffffffff914830450221009bb01f5c43e691dd74d5e6e1a100127481bc3df85be0353269eb3ec247aa90d9022064b34c3d1c95c105e3477f67300225d31f725aec606b5ce73b4972c42e31c37501475121032c4c0ec5caf4e2ae7df7d6d69757b549ba7c3c3f415d4768fb4e4ef27776cc2a2102325ee7f7b05557eee48d7663f3fe25a77f5343f22e9b0c70af65c70ba508114e52ae`},
}

func TestAddTicket(t *testing.T) {
	for _, test := range addTicketTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		msg := wire.NewMsgTx()
		b, err := hex.DecodeString(test.wireMsg)
		if err != nil {
			t.Error(err)
			return
		}
		err = msg.FromBytes(b)
		if err != nil {
			t.Error(err)
			return
		}
		tx := dcrutil.NewTx(msg)
		err = wallet.AddTicket(nil, tx)
		if err != nil {
			t.Error(err)
			return
		}
	}
}

type createMultisigTest struct {
	res     string
	req     string
	reqSigs int
	addr1   string
	addr2   string
	hash    string
}

var createMultisigTests = []createMultisigTest{
	{
		req:     `{"jsonrpc":"2.0","method":"createmultisig","params":[2,["Tsejf7AUnDMGEawzDnQkTmLvYzgb45CyX6y","TskauEiTUQYVZnN6debqguaJSd4oPwbRcnA"]],"id":0}`,
		res:     `{"address":"Tcn1ymQ4jh1ghXbYmwocqpvhSr2n4XsE1Ae","redeemScript":"52210221d306bbc717dd26d47623e7eeae5c04ef64bdc06fa77c96be46f6a82e6ccea021021889e90c6c1e76eb0c9a534a7de61b1aa1e349b57fdf213fc03ddc476a0e01df52ae"}`,
		reqSigs: 2,
		addr1:   `Tsejf7AUnDMGEawzDnQkTmLvYzgb45CyX6y`,
		addr2:   `TskauEiTUQYVZnN6debqguaJSd4oPwbRcnA`,
		hash:    `Tcn1ymQ4jh1ghXbYmwocqpvhSr2n4XsE1Ae`},
}

func TestCreateMultisig(t *testing.T) {
	for _, test := range createMultisigTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		addr1, err := dcrutil.DecodeAddress(test.addr1, chaincfg.TestNet3Params())
		if err != nil {
			t.Error(err)
			return
		}
		addr2, err := dcrutil.DecodeAddress(test.addr2, chaincfg.TestNet3Params())
		if err != nil {
			t.Error(err)
			return
		}
		addresses := []dcrutil.Address{addr1, addr2}
		ms, err := wallet.CreateMultisig(nil, test.reqSigs, addresses)
		if err != nil {
			t.Error(err)
			return
		}
		if ms.Address != test.hash {
			t.Errorf("expected hash %v does not match actual %v", test.hash, ms.Address)
			return
		}
	}
}

type generateVoteTest struct {
	res        string
	req        string
	blockHash  string
	height     int64
	ticketHash string
	votebits   uint16
	voteConfig string
	hex        string
}

var generateVoteTests = []generateVoteTest{
	{
		req:        `{"jsonrpc":"2.0","method":"generatevote","params":["000000170a20de418e4e472b3a9dc1c30bd1fdbeb654dd3eb2c987af8b8bb73b",288425,"e2e7ecc2d888a79cea374bcdf747dda091120a98e49de06ac5571d79a7cca8e4",1,"07000000"],"id":0}`,
		res:        `{"hex":"01000000020000000000000000000000000000000000000000000000000000000000000000ffffffff00ffffffffe4a8cca7791d57c56ae09de4980a1291a0dd47f7cd4b37ea9ca788d8c2ece7e20000000001ffffffff0400000000000000000000266a243bb78b8baf87c9b23edd54b6befdd10bc3c19d3a2b474e8e41de200a17000000a966040000000000000000000000086a060100070000004fd002000000000000001abb76a914bea72121c85806e74ed80f821079719a173a61d988ac6fe1b67b0200000000001abb76a914844daa6df90cdc82e015054f70a017b39365a23088ac000000000000000002a45a38020000000000000000ffffffff0200001b5781790200000000000000ffffffff90473044022053b3c3c511484b1e16747f1350c4c16d50d8faba8c186c86b982697c04516262022076e9c944c884622af88a151e55c637dafa1fc62a340d9e1008236d966fa42ad20147512103b674fbeecb4e10f8fc67441f2fd3396c9629c7e6b47d1008a64e7d8ed6bb1b3b21026d11c3316c0305c0edfbffcea8e758e1a87210a00ef7e2db65d7581ed48d52f852ae"}`,
		blockHash:  "000000170a20de418e4e472b3a9dc1c30bd1fdbeb654dd3eb2c987af8b8bb73b",
		height:     288425,
		ticketHash: "e2e7ecc2d888a79cea374bcdf747dda091120a98e49de06ac5571d79a7cca8e4",
		votebits:   1,
		voteConfig: "07000000",
		hex:        "01000000020000000000000000000000000000000000000000000000000000000000000000ffffffff00ffffffffe4a8cca7791d57c56ae09de4980a1291a0dd47f7cd4b37ea9ca788d8c2ece7e20000000001ffffffff0400000000000000000000266a243bb78b8baf87c9b23edd54b6befdd10bc3c19d3a2b474e8e41de200a17000000a966040000000000000000000000086a060100070000004fd002000000000000001abb76a914bea72121c85806e74ed80f821079719a173a61d988ac6fe1b67b0200000000001abb76a914844daa6df90cdc82e015054f70a017b39365a23088ac000000000000000002a45a38020000000000000000ffffffff0200001b5781790200000000000000ffffffff90473044022053b3c3c511484b1e16747f1350c4c16d50d8faba8c186c86b982697c04516262022076e9c944c884622af88a151e55c637dafa1fc62a340d9e1008236d966fa42ad20147512103b674fbeecb4e10f8fc67441f2fd3396c9629c7e6b47d1008a64e7d8ed6bb1b3b21026d11c3316c0305c0edfbffcea8e758e1a87210a00ef7e2db65d7581ed48d52f852ae"},
}

func TestGenerateVote(t *testing.T) {
	for _, test := range generateVoteTests {
		bh, err := chainhash.NewHashFromStr(test.blockHash)
		if err != nil {
			t.Error(err)
			return
		}
		th, err := chainhash.NewHashFromStr(test.ticketHash)
		if err != nil {
			t.Error(err)
			return
		}
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		res, err := wallet.GenerateVote(nil, bh, test.height, th, test.votebits, test.voteConfig)
		if err != nil {
			t.Error(err)
			return
		}
		if res.Hex != test.hex {
			t.Errorf("expected hex %v does not match actual %v", test.hex, res.Hex)
			return
		}
	}
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

type getStakeInfoTest struct {
	res         string
	req         string
	blockHeight int64
	voted       uint32
}

var getStakeInfoTests = []getStakeInfoTest{
	{
		req:         `{"jsonrpc":"2.0","method":"getstakeinfo","id":0}`,
		res:         `{"blockheight":389345,"difficulty":137.50528146,"totalsubsidy":1.01000277,"ownmempooltix":0,"immature":0,"unspent":1,"voted":1,"revoked":0,"unspentexpired":0,"poolsize":40600,"allmempooltix":15,"live":1,"proportionlive":0.000024630541871921184}`,
		blockHeight: 389345,
		voted:       1},
}

func TestGetStakeInfo(t *testing.T) {
	for _, test := range getStakeInfoTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		info, err := wallet.GetStakeInfo(nil)
		if err != nil {
			t.Error(err)
			return
		}
		if info.BlockHeight != test.blockHeight {
			t.Errorf("expected block height %v does not match actual %v", test.blockHeight, info.BlockHeight)
			return
		}
		if info.Voted != test.voted {
			t.Errorf("expected voted %v does not match actual %v", test.voted, info.Voted)
			return
		}
	}
}

type getTicketsTest struct {
	res             string
	req             string
	ticket          string
	includeImmature bool
}

var getTicketsTests = []getTicketsTest{
	{
		req:             `{"jsonrpc":"2.0","method":"gettickets","params":[false],"id":0}`,
		res:             `{"hashes":["c852b0710183fedd7cda50dfaec4b253cfa6ae7b9d5565f3df8c2e44e6234121","27a8888bf3c0222db58405c4f877959fa916a4ff439ad34a3b90f0a53297234a","5f3bce73af7ea53b2a134098ec9e589c767b5d1c14c7f2e8e81e2829d4bda272","44483ebf9ebd60f9d6236fc6a6756a068ea3d77f46d326d78c1cfdca0b6d858d","11f72643209e922896ff30e24b8ccdeb1e8e851f9aa20067dffc1d57c80b0a9e","c9d9a69bb8b6da6831f784c497ede87e2f83054d5986c6961ba9badcfc608cea"]}`,
		ticket:          `c852b0710183fedd7cda50dfaec4b253cfa6ae7b9d5565f3df8c2e44e6234121`,
		includeImmature: false},
}

func TestGetTickets(t *testing.T) {
	for _, test := range getTicketsTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		tickets, err := wallet.GetTickets(nil, test.includeImmature)
		if err != nil {
			t.Error(err)
			return
		}
		if tickets[0].String() != test.ticket {
			t.Errorf("expected ticket hash %v does not match actual %v", test.ticket, tickets[0].String())
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
		req:  `{"jsonrpc":"2.0","method":"gettransaction","params":["5072057ab5a8f4ea54de448d87f1c46da77fd63bd5b1ebf157d4b38e2630a643"],"id":0}`,
		res:  `{"amount":-65.36198367,"confirmations":46144,"blockhash":"0000006135bfcebc79b1ddd07aff900e444d03d5cf7760b329fbcea1b018a6ce","blockindex":0,"blocktime":1564706999,"txid":"5072057ab5a8f4ea54de448d87f1c46da77fd63bd5b1ebf157d4b38e2630a643","walletconflicts":[],"time":1564706376,"timereceived":1564706376,"details":[{"account":"","amount":-0,"category":"send","fee":0,"vout":0},{"account":"","amount":-0,"category":"send","fee":0,"vout":1},{"account":"","address":"TsnYwvpcmJQVQgqVvKHJq4wHPqf3ReJLRnh","amount":-0.00231829,"category":"send","fee":0,"vout":2},{"account":"","address":"TsdgeGsZNNHMf6JpfciKLhk3skvbz8QWBAo","amount":-65.82792908,"category":"send","fee":0,"vout":3}],"hex":"01000000020000000000000000000000000000000000000000000000000000000000000000ffffffff00ffffffff3de61810953e35f7a5b964f480149966adf4291a7c0746d74ff5701a035e24060000000001ffffffff0400000000000000000000266a24e86443a47039c2a1210504548bfb44a17ca9b492f5df08109c918bb3ba00000065ae030000000000000000000000086a06010007000000958903000000000000001abb76a914ec2ebb5e5313fac4faad85d26291dc615e4eb4b188accc725d880100000000001abb76a9148aea30c376345e7f5d32ab5a100be4f6bc9a915a88ac0000000000000000028383ca020000000000000000ffffffff020000df7896850100000000000000ffffffff914830450221009bb01f5c43e691dd74d5e6e1a100127481bc3df85be0353269eb3ec247aa90d9022064b34c3d1c95c105e3477f67300225d31f725aec606b5ce73b4972c42e31c37501475121032c4c0ec5caf4e2ae7df7d6d69757b549ba7c3c3f415d4768fb4e4ef27776cc2a2102325ee7f7b05557eee48d7663f3fe25a77f5343f22e9b0c70af65c70ba508114e52ae","type":""}`,
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
		res, err := wallet.GetTransaction(nil, hash)
		if err != nil {
			t.Error(err)
			return
		}
		if res.TxID != test.hash {
			t.Errorf("expected hash %v does not match actual %v", test.hash, res.TxID)
			return
		}
	}
}

func TestGetTransactionAsync(t *testing.T) {
	for _, test := range getTransactionTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		hash, err := chainhash.NewHashFromStr(test.hash)
		if err != nil {
			t.Error(err)
			return
		}
		f := wallet.GetTransactionAsync(nil, hash)
		res, err := f()
		if err != nil {
			t.Error(err)
			return
		}
		if res.TxID != test.hash {
			t.Errorf("expected hash %v does not match actual %v", test.hash, res.TxID)
			return
		}
	}
}

type importScriptRescanFromTest struct {
	res      string
	req      string
	script   string
	rescan   bool
	scanFrom int
}

var importScriptRescanFromTests = []importScriptRescanFromTest{
	{
		req:      `{"jsonrpc":"2.0","method":"importscript","params":["5121032c4c0ec5caf4e2ae7df7d6d69757b549ba7c3c3f415d4768fb4e4ef27776cc2a2102325ee7f7b05557eee48d7663f3fe25a77f5343f22e9b0c70af65c70ba508114e52ae",true,5000],"id":0}`,
		res:      `null`,
		script:   `5121032c4c0ec5caf4e2ae7df7d6d69757b549ba7c3c3f415d4768fb4e4ef27776cc2a2102325ee7f7b05557eee48d7663f3fe25a77f5343f22e9b0c70af65c70ba508114e52ae`,
		rescan:   true,
		scanFrom: 5000},
}

func TestImportScriptRescanFrom(t *testing.T) {
	for _, test := range importScriptRescanFromTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		script, err := hex.DecodeString(test.script)
		if err != nil {
			t.Error(err)
			return
		}
		err = wallet.ImportScriptRescanFrom(nil, script, test.rescan, test.scanFrom)
		if err != nil {
			t.Error(err)
			return
		}
	}
}

type listScriptsTest struct {
	res          string
	req          string
	redeemScript string
}

var listScriptsTests = []listScriptsTest{
	{
		req:          `{"jsonrpc":"2.0","method":"listscripts","id":0}`,
		res:          `{"scripts":[{"hash160":"0201168337ea07885159f7cca5963639bdf6a04a","address":"TcXhQhWxCMP6kKufyxRGNuGTArNNaBA4PUB","redeemscript":"512103efae5ad4236405f666e007dea108642a30457c82c28daec69968fba499258c392102b8318f22d12f9ec90bf3ce92574a28d143c8e18300aba0a41594268913fddf9a52ae"},{"hash160":"515d6c01df8f3d908da72da81204b51b26a864da","address":"Tcew2h9YV4Lba5JTJScUL94huH7bjAGJY1M","redeemscript":"5121033ae39f47ab054b021da6d7dba7d9f208b871abc72db291926e31e7fa498ea629210346f9ee53d64336673d165ff88d5e7c555884599815b843fea0afa835a80b921f52ae"}]}`,
		redeemScript: `512103efae5ad4236405f666e007dea108642a30457c82c28daec69968fba499258c392102b8318f22d12f9ec90bf3ce92574a28d143c8e18300aba0a41594268913fddf9a52ae`},
}

func TestListScripts(t *testing.T) {
	for _, test := range listScriptsTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		scripts, err := wallet.ListScripts(nil)
		if err != nil {
			t.Error(err)
			return
		}
		script := hex.EncodeToString(scripts[0])
		if script != test.redeemScript {
			t.Errorf("expected redeem script %v does not match actual %v", test.redeemScript, script)
			return
		}
	}
}

type stakePoolUserInfoTest struct {
	res    string
	req    string
	msa    string
	ticket string
}

var stakePoolUserInfoTests = []stakePoolUserInfoTest{
	{
		req:    `{"jsonrpc":"2.0","method":"stakepooluserinfo","params":["TcuyUzpxQMtpfwpM2zS7DGPznxBWV5kxqVf"],"id":0}`,
		res:    `{"tickets":[{"status":"voted","ticket":"317f94d04e9ea857ef64e9415224b6c298e78796de80d59842abe5e54bb0876d","ticketheight":222217,"spentby":"1d17b2429f2e05d55db66d88207d23a63a092457f9cc59ae1a4ebf0f4cbe2fba","spentbyheight":222635},{"status":"voted","ticket":"56b9a5fcdbc3af58112657d935de089afd89a868432a06f1f0dd1a9046fd7840","ticketheight":222600,"spentby":"c92165095c117bc78f4d22d58cd880644dcfa892117586b94a84a25d7608d46e","spentbyheight":225919},{"status":"voted","ticket":"bd2b70056f27ed3c59a58217ba3ceb9c6e5875738949f46eaaa1abd0d558ae7a","ticketheight":222600,"spentby":"c91557e8e2ec17c66db672f836bdd2604dd1a3921663078cd82cc64cbcc2bb00","spentbyheight":224289},{"status":"voted","ticket":"2d8278f097fd90c0418d79a0ffd2b55aaef522784bd00881e8915b7cedd3e08e","ticketheight":222600,"spentby":"9273d667561b93500aa48c07ef2e92c25675ae4e449f2e3411883e11bc5712dd","spentbyheight":222644},{"status":"voted","ticket":"6cd3ee1648dd55e3a0fb94f5e16c29d2c4af1e80ba2fa31d3bf5e0e2cbd49c75","ticketheight":222600,"spentby":"89b7235f38033126790a1731676ad2817dbbdbe039c2ce113bb1ebae432470ec","spentbyheight":222865},{"status":"voted","ticket":"edadfda33918db681df2926bf96122c96fdca45c19dcbcd26d8aac95a2a93388","ticketheight":222601,"spentby":"b665af60350d481bc5ad7214da1d0e4e8e5586e3e4617671771ca2664f3f0a1a","spentbyheight":225128},{"status":"voted","ticket":"897234cdff41149ac565b3ae928ccfdb711a5c5873bc054b93c3da115f7c7aee","ticketheight":222601,"spentby":"db556314cd8fa7eb7d9ef9dd2e54ef7524047d9cb28ac0787c10f75e1d519aa1","spentbyheight":223029}],"invalid":[]}`,
		msa:    `TcuyUzpxQMtpfwpM2zS7DGPznxBWV5kxqVf`,
		ticket: `317f94d04e9ea857ef64e9415224b6c298e78796de80d59842abe5e54bb0876d`},
}

func TestStakePoolUserInfo(t *testing.T) {
	for _, test := range stakePoolUserInfoTests {
		msa, err := dcrutil.DecodeAddress(test.msa, chaincfg.TestNet3Params())
		if err != nil {
			t.Error(err)
			return
		}
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		info, err := wallet.StakePoolUserInfo(nil, msa)
		if err != nil {
			t.Error(err)
			return
		}
		ticket := info.Tickets[0].Ticket
		if ticket != test.ticket {
			t.Errorf("expected ticket hash %v does not match actual %v", test.ticket, ticket)
			return
		}
	}
}

type validateAddressTest struct {
	res           string
	req           string
	address       string
	pubKeyAddress string
}

var validateAddressTests = []validateAddressTest{
	{
		req:           `{"jsonrpc":"2.0","method":"validateaddress","params":["Tsejf7AUnDMGEawzDnQkTmLvYzgb45CyX6y"],"id":0}`,
		res:           `{"isvalid":true,"address":"Tsejf7AUnDMGEawzDnQkTmLvYzgb45CyX6y","ismine":true,"pubkeyaddr":"TkKkopSgMBfzoozzbNjcxp3wrc2jEWC8MyMty3tQXFMXHR5CuzNJt","pubkey":"0221d306bbc717dd26d47623e7eeae5c04ef64bdc06fa77c96be46f6a82e6ccea0","iscompressed":true,"account":"default"}`,
		address:       `Tsejf7AUnDMGEawzDnQkTmLvYzgb45CyX6y`,
		pubKeyAddress: `TkKkopSgMBfzoozzbNjcxp3wrc2jEWC8MyMty3tQXFMXHR5CuzNJt`},
}

func TestValidateAddress(t *testing.T) {
	for _, test := range validateAddressTests {
		addr, err := dcrutil.DecodeAddress(test.address, chaincfg.TestNet3Params())
		if err != nil {
			t.Error(err)
			return
		}
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		validated, err := wallet.ValidateAddress(nil, addr)
		if err != nil {
			t.Error(err)
			return
		}
		if validated.PubKeyAddr != test.pubKeyAddress {
			t.Errorf("expected pubkey address %v does not match actual %v", test.pubKeyAddress, validated.PubKeyAddr)
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
		res: `{"dcrd":{"versionstring":"1.5.0-pre+dev","major":1,"minor":5,"patch":0,"prerelease":"pre","buildmetadata":"dev.go1-12-7"},"dcrdjsonrpcapi":{"versionstring":"6.1.0","major":6,"minor":1,"patch":0,"prerelease":"","buildmetadata":""},"dcrwalletjsonrpcapi":{"versionstring":"6.2.0","major":6,"minor":2,"patch":0,"prerelease":"","buildmetadata":""}}`,
		ver: `6.2.0`},
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
		v := ver["dcrwalletjsonrpcapi"]
		vStr := fmt.Sprintf("%v.%v.%v", v.Major, v.Minor, v.Patch)
		if vStr != test.ver {
			t.Errorf("expected version %v does not match actual %v", test.ver, vStr)
			return
		}
	}
}

type walletInfoTest struct {
	res      string
	req      string
	cointype uint32
}

var walletInfoTests = []walletInfoTest{
	{
		req:      `{"jsonrpc":"2.0","method":"walletinfo","id":0}`,
		res:      `{"daemonconnected":true,"unlocked":true,"cointype":1,"txfee":0.0001,"ticketfee":0.0001,"ticketpurchasing":false,"votebits":1,"votebitsextended":"07000000","voteversion":7,"voting":true}`,
		cointype: 1},
}

func TestWalletInfo(t *testing.T) {
	for _, test := range walletInfoTests {
		m := &mockRPC{res: test.res, req: test.req}
		wallet := New(m)
		info, err := wallet.WalletInfo(nil)
		if err != nil {
			t.Error(err)
			return
		}
		if info.CoinType != test.cointype {
			t.Errorf("expected coin type %v does not match actual %v", test.cointype, info.CoinType)
			return
		}
	}
}
