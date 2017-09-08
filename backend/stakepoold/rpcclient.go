package main

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrstakepool/backend/stakepoold/userdata"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/wallet/txrules"
)

var requiredChainServerAPI = semver{major: 3, minor: 1, patch: 0}
var requiredWalletAPI = semver{major: 4, minor: 1, patch: 0}

func connectNodeRPC(ctx *appContext, cfg *config) (*dcrrpcclient.Client, semver, error) {
	var nodeVer semver

	dcrdCert, err := ioutil.ReadFile(cfg.DcrdCert)
	if err != nil {
		log.Errorf("Failed to read dcrd cert file at %s: %s\n",
			cfg.DcrdCert, err.Error())
		return nil, nodeVer, err
	}

	log.Debugf("Attempting to connect to dcrd RPC %s as user %s "+
		"using certificate located in %s",
		cfg.DcrdHost, cfg.DcrdUser, cfg.DcrdCert)

	connCfgDaemon := &dcrrpcclient.ConnConfig{
		Host:         cfg.DcrdHost,
		Endpoint:     "ws", // websocket
		User:         cfg.DcrdUser,
		Pass:         cfg.DcrdPassword,
		Certificates: dcrdCert,
	}

	ntfnHandlers := getNodeNtfnHandlers(ctx, connCfgDaemon)
	dcrdClient, err := dcrrpcclient.New(connCfgDaemon, ntfnHandlers)
	if err != nil {
		log.Errorf("Failed to start dcrd RPC client: %s\n", err.Error())
		return nil, nodeVer, err
	}

	// Ensure the RPC server has a compatible API version.
	ver, err := dcrdClient.Version()
	if err != nil {
		log.Error("Unable to get RPC version: ", err)
		return nil, nodeVer, fmt.Errorf("Unable to get node RPC version")
	}

	dcrdVer := ver["dcrdjsonrpcapi"]
	nodeVer = semver{dcrdVer.Major, dcrdVer.Minor, dcrdVer.Patch}

	if !semverCompatible(requiredChainServerAPI, nodeVer) {
		return nil, nodeVer, fmt.Errorf("Node JSON-RPC server does not have "+
			"a compatible API version. Advertises %v but require %v",
			nodeVer, requiredChainServerAPI)
	}

	return dcrdClient, nodeVer, nil
}

func connectWalletRPC(cfg *config) (*dcrrpcclient.Client, semver, error) {
	var walletVer semver

	dcrwCert, err := ioutil.ReadFile(cfg.WalletCert)
	if err != nil {
		log.Errorf("Failed to read dcrwallet cert file at %s: %s\n",
			cfg.WalletCert, err.Error())
		return nil, walletVer, err
	}

	log.Infof("Attempting to connect to dcrwallet RPC %s as user %s "+
		"using certificate located in %s",
		cfg.WalletHost, cfg.WalletUser, cfg.WalletCert)

	connCfgWallet := &dcrrpcclient.ConnConfig{
		Host:         cfg.WalletHost,
		Endpoint:     "ws",
		User:         cfg.WalletUser,
		Pass:         cfg.WalletPassword,
		Certificates: dcrwCert,
	}

	ntfnHandlers := getWalletNtfnHandlers(cfg)
	dcrwClient, err := dcrrpcclient.New(connCfgWallet, ntfnHandlers)
	if err != nil {
		log.Errorf("Verify that username and password is correct and that "+
			"rpc.cert is for your wallet: %v", cfg.WalletCert)
		return nil, walletVer, err
	}

	// Ensure the wallet RPC server has a compatible API version.
	ver, err := dcrwClient.Version()
	if err != nil {
		log.Error("Unable to get RPC version: ", err)
		return nil, walletVer, fmt.Errorf("Unable to get node RPC version")
	}

	dcrwVer := ver["dcrwalletjsonrpcapi"]
	walletVer = semver{dcrwVer.Major, dcrwVer.Minor, dcrwVer.Patch}

	if !semverCompatible(requiredWalletAPI, walletVer) {
		return nil, walletVer, fmt.Errorf("Node JSON-RPC server does not have "+
			"a compatible API version. Advertises %v but require %v",
			walletVer, requiredWalletAPI)
	}

	return dcrwClient, walletVer, nil
}

// nodeCheckTicketFee evaluates a stake pool ticket to see if it's
// acceptable to the stake pool. The ticket must pay out to the stake
// pool cold wallet, and must have a sufficient fee.
// This function is a port of evaluateStakePoolTicket in dcrwallet which plucks
// the in/out amounts from the RPC response rather than the wallet's record.
// Stake pool fee tests are present in dcrwallet/wallet/txrules.
func nodeCheckTicketFee(ctx *appContext, txhex string, blockhash string) (bool, error) {
	// Create raw transaction.
	var buf []byte
	buf, err := hex.DecodeString(txhex)
	if err != nil {
		return false, fmt.Errorf("DecodeString failed: %v", err)
	}
	res, err := ctx.nodeConnection.DecodeRawTransaction(buf)
	if err != nil {
		return false, fmt.Errorf("DecodeRawTransaction failed: %v", err)
	}

	bhash, err := chainhash.NewHashFromStr(blockhash)
	if err != nil {
		return false, fmt.Errorf("NewHashFromStr failed for %v: %v", blockhash, err)
	}
	gbh, err := ctx.nodeConnection.GetBlockHeader(bhash)
	if err != nil {
		return false, fmt.Errorf("GetBlockHeader failed for %v: %v", bhash, err)
	}

	// Check the first commitment output (Vout[1])
	// and ensure that the address found there exists
	// in the list of approved addresses. Also ensure
	// that the fee exists and is of the amount
	// requested by the pool.

	commitAmt := dcrutil.Amount(0)
	feeAddr := ""
	feeAddrValid := false
	ticketValue, err := dcrutil.NewAmount(res.Vout[0].Value)
	if err != nil {
		return false, fmt.Errorf("NewAmount failed: %v", err)
	}

	for i := range res.Vout {
		switch res.Vout[i].ScriptPubKey.Type {
		case "sstxcommitment":
			for j := range res.Vout[i].ScriptPubKey.Addresses {
				feeAddr = res.Vout[i].ScriptPubKey.Addresses[j]
				_, exists := ctx.feeAddrs[feeAddr]
				if !exists {
					continue
				}
				feeAddrValid = true
				feeAmount, err := dcrutil.NewAmount(*res.Vout[i].ScriptPubKey.CommitAmt)
				if err != nil {
					return false, fmt.Errorf("NewAmount failed: %v", err)
				}
				commitAmt = feeAmount
				break
			}
		}
	}

	in := dcrutil.Amount(0)
	for i := range res.Vout {
		switch res.Vout[i].ScriptPubKey.Type {
		case "sstxcommitment":
			amount, err := dcrutil.NewAmount(*res.Vout[i].ScriptPubKey.CommitAmt)
			if err != nil {
				return false, fmt.Errorf("NewAmount failed: %v", err)
			}
			in += amount
		}
	}

	out := dcrutil.Amount(0)
	for i := range res.Vout {
		amount, err := dcrutil.NewAmount(res.Vout[i].Value)
		if err != nil {
			return false, fmt.Errorf("NewAmount failed: %v", err)
		}
		out += amount
	}
	fees := in - out

	if !feeAddrValid {
		log.Warnf("Unknown pool commitment address %s for ticket %v",
			feeAddr, res.Txid)
		return false, nil
	}

	// Calculate the fee required based on the current
	// height and the required amount from the pool.
	feeNeeded := txrules.StakePoolTicketFee(ticketValue, fees,
		int32(gbh.Height), ctx.feePercent, ctx.params)
	if commitAmt < feeNeeded {
		log.Warnf("User %s submitted ticket %v which has less fees than "+
			"are required to use this stake pool and is being skipped "+
			"(required: %v, found %v)", feeAddr, res.Txid, feeNeeded, fees)

		// Reject the entire transaction if it didn't
		// pay the pool server fees.
		return false, nil
	}

	log.Debugf("Accepted valid stake pool ticket %v committing %v in fees",
		res.Txid, commitAmt)

	return true, nil
}

func walletGetTickets(ctx *appContext) map[chainhash.Hash]string {
	// This is suboptimal to copy and needs fixing.
	userVotingConfig := make(map[string]userdata.UserVotingConfig)
	ctx.RLock()
	for k, v := range ctx.userVotingConfig {
		userVotingConfig[k] = v
	}
	ctx.RUnlock()

	userTickets := make(map[chainhash.Hash]string)

	log.Info("Running GetTickets, this may take awhile...")
	timenow := time.Now()
	tickets, err := ctx.walletConnection.GetTickets(false)
	log.Infof("GetTickets: took %v", time.Since(timenow))

	if err != nil {
		log.Warnf("GetTickets failed: %v", err)
		return userTickets
	}

	type promise struct {
		dcrrpcclient.FutureGetTransactionResult
	}
	promises := make([]promise, 0, len(tickets))

	log.Debugf("setting up GetTransactionAsync for %v tickets", len(tickets))
	for _, ticket := range tickets {
		// lookup ownership of each ticket
		promises = append(promises, promise{ctx.walletConnection.GetTransactionAsync(ticket)})
	}

	counter := 0
	for _, p := range promises {
		counter++
		log.Debugf("Receiving GetTransaction result for ticket %v/%v", counter, len(tickets))
		gt, err := p.Receive()
		if err != nil {
			// All tickets should exist and be able to be looked up
			log.Warnf("GetTransaction error: %v", err)
			continue
		}
		for i := range gt.Details {
			_, ok := userVotingConfig[gt.Details[i].Address]
			if !ok {
				log.Warnf("Could not map ticket %v to a user, user %v doesn't exist", gt.TxID, gt.Details[i].Address)
				continue
			}

			hash, err := chainhash.NewHashFromStr(gt.TxID)
			if err != nil {
				log.Warnf("invalid ticket %v", err)
				continue
			}

			// we could check fees here but they are currently checked by
			// dcrwallet.  too-low-fee tickets won't show up in GetTickets
			// output until they have been manually added via the addticket
			// RPC
			log.Debugf("added ticket %s for %s", *hash, gt.Details[i].Address)
			userTickets[*hash] = userVotingConfig[gt.Details[i].Address].MultiSigAddress
			break
		}
	}

	log.Infof("%d live tickets ready for voting", len(userTickets))

	return userTickets
}
