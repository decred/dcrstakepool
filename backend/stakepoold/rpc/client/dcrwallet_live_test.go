// +build !live
// Copyright (c) 2020 the Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package client

// This file provides a live test for dcrwallet RPC calls. In order to function,
// dcrwallet must be running on testnet and using its default data directory,
// unlocked, with username and password set in the config file.
//
// WARNING: Will leave extraneous data in your testnet wallet, such as tickets
// and redeem scripts.
//
// NOTE: Tests will fail the first time run as a rescan takes time and the
// transaction we are looking for will not show up until after a rescan.
// To avoid this import the following script and rescan from block 372162
// before attempting:
// 512103af3c24d005ca8b755e7167617f3a5b4c60a65f8318a7fcd1b0cacb1abd2a97fc21027b
// 81bc16954e28adb832248140eb58bedb6078ae5f4dabf21fde5a8ab7135cb652ae
import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"

	flags "github.com/jessevdk/go-flags"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrstakepool/backend/stakepoold/rpc/client/dcrwallet"
)

type config struct {
	Username string `long:"username"`
	Password string `long:"password"`
}

// fileExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

type wallet struct {
	*dcrwallet.RPC
	Conn *Conn
}

var ctx context.Context

// a wallet rpc client
var tw wallet

func connectWalletRPC(ctx context.Context, wg *sync.WaitGroup) error {
	home := dcrutil.AppDataDir("dcrwallet", false)
	conf := filepath.Join(home, "dcrwallet.conf")
	if !fileExists(conf) {
		return errors.New("no config file")
	}
	cert := filepath.Join(home, "rpc.cert")
	dcrwCert, err := ioutil.ReadFile(cert)
	if err != nil {
		return err
	}
	cfg := new(config)
	parser := flags.NewParser(cfg, flags.Default)
	// ignoring errors for extra flags
	_ = flags.NewIniParser(parser).ParseFile(conf)
	host := "127.0.0.1:19110"
	testopts := &RPCOptions{
		Host: host,
		User: cfg.Username,
		Pass: cfg.Password,
		CA:   dcrwCert,
	}
	conn, err := NewConn(ctx, wg, testopts)
	if err != nil {
		return fmt.Errorf("new client connection error: %v", err)
	}
	dcrwClient := dcrwallet.New(conn)
	// Ensure the wallet is reachable.
	_, err = dcrwClient.Version(ctx)
	if err != nil {
		return fmt.Errorf("unable to get wallet RPC version: %v", err)
	}
	tw = wallet{RPC: dcrwClient, Conn: conn}
	return nil
}

// TestMain sets up the websocket client and will stop tests without causing
// tests to fail if there is a problem setting up.
func TestMain(m *testing.M) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	wg := new(sync.WaitGroup)
	defer func() {
		cancel()
		wg.Wait()
	}()
	if err := connectWalletRPC(ctx, wg); err != nil {
		fmt.Printf("skipping live dcrwallet tests: %v\n", err)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestAccountSyncAddressIndex(t *testing.T) {
	err := tw.AccountSyncAddressIndex(ctx, "default", uint32(0), 0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAddTicket(t *testing.T) {
	ticket := "01000000025469d21028333e45f84964f7ea096776eb6460754652ece301c2cbb49feda1460000000000ffffffff5469d21028333e45f84964f7ea096776eb6460754652ece301c2cbb49feda1460100000000ffffffff05f6c87a0802000000000018baa914f2ac4603e9b0925c04625e65258aab87ba57f5838700000000000000000000206a1eb4eba901e16a222dd4f87c82862ab34ee9e807f20bc20100000000000058000000000000000000001abd76a914000000000000000000000000000000000000000088ac00000000000000000000206a1e51a91ad97f474e1e4f3e8c667258803dbda22e58171c7908020000000058000000000000000000001abd76a914000000000000000000000000000000000000000088ac0000000000000000020bc201000000000068da0500020000006a4730440220537f4cb81c308bb48514ad062f19f4c9f3ab76341aa11ddcf5186b7c440397020220119dd6c6c538c045bfd038cd6b5c69c49e817f90f3b85dae03ce8e6a3ac3012601210294a1ac258908daf87907d02d68e15aee88cb836c85dd0c2ab85f972f0291ac54171c79080200000068da0500020000006a4730440220755cd9ed7476452d248e6e3981dae11aa6eb18bba337405c5d38b0d26f838bc0022067c17538660a151738156a0762d6fec15cee8ee33873978e4fd8594b4add680c01210294a1ac258908daf87907d02d68e15aee88cb836c85dd0c2ab85f972f0291ac54"
	b, err := hex.DecodeString(ticket)
	if err != nil {
		t.Fatal(err)
	}
	msg := wire.NewMsgTx()
	err = msg.FromBytes(b)
	if err != nil {
		t.Error(err)
		return
	}
	tx := dcrutil.NewTx(msg)
	err = tw.AddTicket(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
}

var scriptHex string
var msaHex string

func TestCreateMultisig(t *testing.T) {
	addr1 := `TkQ4hPAjU7fWxNgTdNxDXzrGQc1r4w2yjeM667Rkxa6MtsgcJmNpu`
	addr2 := `TkKmVKG7u7PwhQaYr7wgMqBwHneJ2cN4e5YpMVUsWSopx81NFXEzK`
	msaHex = `Tcbvn2hiEAXBDwUPDLDG2SxF9iANMKhdVev`
	a1, err := dcrutil.DecodeAddress(addr1, chaincfg.TestNet3Params())
	if err != nil {
		t.Error(err)
		return
	}
	a2, err := dcrutil.DecodeAddress(addr2, chaincfg.TestNet3Params())
	if err != nil {
		t.Error(err)
		return
	}
	addrs := []dcrutil.Address{a1, a2}

	res, err := tw.CreateMultisig(ctx, 1, addrs)
	if err != nil {
		t.Fatal(err)
	}
	if res.Address != msaHex {
		t.Fatalf("wanted %v but got %v for msa", msaHex, res.Address)
	}
	scriptHex = res.RedeemScript
}

func TestGenerateVote(t *testing.T) {
	blockHash := "0000003a54268af24b58e95943c8ab391e75ea43f327743fed88af2a75a273be"
	ticketHash := "e6f45d637233b27d539d81ba3915a4e2a9bb188c01685b19d819ae91debe291c"
	hex := "01000000020000000000000000000000000000000000000000000000000000000000000000ffffffff00ffffffff1c29bede91ae19d8195b68018c18bba9e2a41539ba819d537db23372635df4e60000000001ffffffff0400000000000000000000266a24be73a2752aaf88ed3f7427f343ea751e39abc84359e9584bf28a26543a000000a966040000000000000000000000086a06010007000000b1df01000000000000001abb76a914781f472a926da0bb7ce9eec7f4d434de21015cae88ac3ff2c3070200000000001abb76a914f90abbb67dc9257efa6ab24eb88e2755f34b1f7f88ac000000000000000002a45a38020000000000000000ffffffff0200004d778d050200000000000000ffffffff4847512103af3c24d005ca8b755e7167617f3a5b4c60a65f8318a7fcd1b0cacb1abd2a97fc21027b81bc16954e28adb832248140eb58bedb6078ae5f4dabf21fde5a8ab7135cb652ae"
	bh, err := chainhash.NewHashFromStr(blockHash)
	if err != nil {
		t.Error(err)
		return
	}
	th, err := chainhash.NewHashFromStr(ticketHash)
	if err != nil {
		t.Error(err)
		return
	}
	res, err := tw.GenerateVote(ctx, bh, 288425, th, 1, "07000000")
	if err != nil {
		t.Fatalf("unable to generate vote: %v", err)
	}
	if res.Hex != hex {
		t.Errorf("expected hex %v does not match actual %v", hex, res.Hex)
		return
	}
}

func TestGetBestBlock(t *testing.T) {
	bh, _, err := tw.GetBestBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(bh.String()) != 32*2 {
		t.Fatal("unable to get best block hash")
	}
}

func TestGetStakeInfo(t *testing.T) {
	info, err := tw.GetStakeInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.BlockHeight == 0 {
		t.Fatal("unable to get stake info block height")
	}
}

func TestGetTickets(t *testing.T) {
	_, err := tw.GetTickets(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetTransaction(t *testing.T) {
	hash := "e6f45d637233b27d539d81ba3915a4e2a9bb188c01685b19d819ae91debe291c"
	h, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tw.GetTransaction(ctx, h)
	if err != nil {
		t.Fatalf("unable to get transaction: %v", err)
	}
	if res.TxID != hash {
		t.Fatalf("expected hash %v does not match actual %v", hash, res.TxID)
	}
}

func TestGetTransactionAsync(t *testing.T) {
	hash := "e6f45d637233b27d539d81ba3915a4e2a9bb188c01685b19d819ae91debe291c"
	h, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		t.Fatal(err)
	}
	resFunc := tw.GetTransactionAsync(ctx, h)
	res, err := resFunc()
	if err != nil {
		t.Fatalf("unable to get transaction: %v", err)
	}
	if res.TxID != hash {
		t.Fatalf("expected hash %v does not match actual %v", hash, res.TxID)
	}
}

func TestImportScriptRescanFrom(t *testing.T) {
	script, err := hex.DecodeString(scriptHex)
	if err != nil {
		t.Fatal(err)
	}
	err = tw.ImportScriptRescanFrom(ctx, script, true, 372161)
	if err != nil {
		t.Fatal(err)
	}
}

func TestListScripts(t *testing.T) {
	scripts, err := tw.ListScripts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fail := true
	for _, s := range scripts {
		if scriptHex == hex.EncodeToString(s) {
			fail = false
			break
		}
	}
	if fail {
		t.Fatal("unable to find imported script")
	}
}

func TestStakePoolUserInfo(t *testing.T) {
	msa, err := dcrutil.DecodeAddress(msaHex, chaincfg.TestNet3Params())
	if err != nil {
		t.Fatal(err)
	}
	_, err = tw.StakePoolUserInfo(ctx, msa)
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateAddress(t *testing.T) {
	addr, err := dcrutil.DecodeAddress(msaHex, chaincfg.TestNet3Params())
	if err != nil {
		t.Fatal(err)
	}
	validated, err := tw.ValidateAddress(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if validated.IsMine != true {
		t.Fatal("is not mine")
	}
}

func TestVersion(t *testing.T) {
	version, err := tw.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := version["dcrwalletjsonrpcapi"]; !exists {
		t.Fatal("version result does not contain dcrwalletjsonrpcapi")
	}
}

func TestWalletInfo(t *testing.T) {
	info, err := tw.WalletInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.CoinType != 1 {
		t.Fatal("unable to get coin type")
	}
}
