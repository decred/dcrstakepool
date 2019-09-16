package main

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/rpcclient/v3"
	"github.com/decred/dcrstakepool/backend/stakepoold/rpc/rpcserver"
	"github.com/decred/dcrstakepool/backend/stakepoold/userdata"
)

var requiredChainServerAPI = semver{major: 6, minor: 0, patch: 0}
var requiredWalletAPI = semver{major: 6, minor: 0, patch: 1}

func connectNodeRPC(ctx *rpcserver.AppContext, cfg *config) (*rpcclient.Client, semver, error) {
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

	connCfgDaemon := &rpcclient.ConnConfig{
		Host:         cfg.DcrdHost,
		Endpoint:     "ws", // websocket
		User:         cfg.DcrdUser,
		Pass:         cfg.DcrdPassword,
		Certificates: dcrdCert,
	}

	ntfnHandlers := getNodeNtfnHandlers(ctx)
	dcrdClient, err := rpcclient.New(connCfgDaemon, ntfnHandlers)
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

func connectWalletRPC(cfg *config) (*rpcserver.Client, semver, error) {
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

	connCfgWallet := &rpcclient.ConnConfig{
		Host:                 cfg.WalletHost,
		Endpoint:             "ws",
		User:                 cfg.WalletUser,
		Pass:                 cfg.WalletPassword,
		Certificates:         dcrwCert,
		DisableAutoReconnect: true,
	}

	ntfnHandlers := getWalletNtfnHandlers()

	// New also starts an autoreconnect function.
	dcrwClient, err := rpcserver.NewClient(connCfgWallet, ntfnHandlers)
	if err != nil {
		log.Errorf("Verify that username and password is correct and that "+
			"rpc.cert is for your wallet: %v", cfg.WalletCert)
		return nil, walletVer, err
	}

	// Ensure the wallet RPC server has a compatible API version.
	ver, err := dcrwClient.RPCClient().Version()
	if err != nil {
		log.Error("Unable to get RPC version: ", err)
		return nil, walletVer, fmt.Errorf("Unable to get node RPC version")
	}

	dcrwVer := ver["dcrwalletjsonrpcapi"]
	walletVer = semver{dcrwVer.Major, dcrwVer.Minor, dcrwVer.Patch}

	if !semverCompatible(requiredWalletAPI, walletVer) {
		log.Errorf("Node JSON-RPC server %v does not have "+
			"a compatible API version. Advertizes %v but require %v",
			cfg.WalletHost, walletVer, requiredWalletAPI)
		return nil, walletVer, fmt.Errorf("Incompatible dcrwallet RPC version")
	}

	return dcrwClient, walletVer, nil
}

func walletGetTickets(ctx *rpcserver.AppContext) (map[chainhash.Hash]string, map[chainhash.Hash]string, error) {
	blockHashToHeightCache := make(map[chainhash.Hash]int32)

	// This is suboptimal to copy and needs fixing.
	userVotingConfig := make(map[string]userdata.UserVotingConfig)
	ctx.RLock()
	for k, v := range ctx.UserVotingConfig {
		userVotingConfig[k] = v
	}
	ctx.RUnlock()

	ignoredLowFeeTickets := make(map[chainhash.Hash]string)
	liveTickets := make(map[chainhash.Hash]string)
	var normalFee int

	log.Info("Calling GetTickets...")
	timenow := time.Now()
	tickets, err := ctx.WalletConnection.RPCClient().GetTickets(false)
	log.Infof("GetTickets: took %v", time.Since(timenow))

	if err != nil {
		log.Warnf("GetTickets failed: %v", err)
		return ignoredLowFeeTickets, liveTickets, err
	}

	type promise struct {
		rpcclient.FutureGetTransactionResult
	}
	promises := make([]promise, 0, len(tickets))

	log.Debugf("setting up GetTransactionAsync for %v tickets", len(tickets))
	for _, ticket := range tickets {
		// lookup ownership of each ticket
		promises = append(promises, promise{ctx.WalletConnection.RPCClient().GetTransactionAsync(ticket)})
	}

	var counter int
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
			addr := gt.Details[i].Address
			_, ok := userVotingConfig[addr]
			if !ok {
				log.Warnf("Could not map ticket %v to a user, user %v doesn't exist", gt.TxID, addr)
				continue
			}

			hash, err := chainhash.NewHashFromStr(gt.TxID)
			if err != nil {
				log.Warnf("invalid ticket %v", err)
				continue
			}

			// All tickets are present in the GetTickets response, whether they
			// pay the correct fee or not.  So we need to verify fees and
			// sort the tickets into their respective maps.
			_, isAdded := ctx.AddedLowFeeTicketsMSA[*hash]
			if isAdded {
				liveTickets[*hash] = userVotingConfig[addr].MultiSigAddress
			} else {

				msgTx, err := rpcserver.MsgTxFromHex(gt.Hex)
				if err != nil {
					log.Warnf("MsgTxFromHex failed for %v: %v", gt.Hex, err)
					continue
				}

				// look up the height at which this ticket was purchased
				var ticketBlockHeight int32
				ticketBlockHash, err := chainhash.NewHashFromStr(gt.BlockHash)
				if err != nil {
					log.Warnf("NewHashFromStr failed for %v: %v", gt.BlockHash, err)
					continue
				}

				height, inCache := blockHashToHeightCache[*ticketBlockHash]
				if inCache {
					ticketBlockHeight = height
				} else {
					gbh, err := ctx.NodeConnection.GetBlockHeader(ticketBlockHash)
					if err != nil {
						log.Warnf("GetBlockHeader failed for %v: %v", ticketBlockHash, err)
						continue
					}

					blockHashToHeightCache[*ticketBlockHash] = int32(gbh.Height)
					ticketBlockHeight = int32(gbh.Height)
				}

				ticketFeesValid, err := ctx.EvaluateStakePoolTicket(msgTx, ticketBlockHeight)

				if err != nil {
					log.Warnf("ignoring ticket %v for multisig %v due to error: %v",
						*hash, ctx.UserVotingConfig[addr].MultiSigAddress, err)
					ignoredLowFeeTickets[*hash] = userVotingConfig[addr].MultiSigAddress
				} else if ticketFeesValid {
					normalFee++
					liveTickets[*hash] = userVotingConfig[addr].MultiSigAddress
				} else {
					log.Warnf("ignoring ticket %v for multisig %v due to invalid fee",
						*hash, ctx.UserVotingConfig[addr].MultiSigAddress)
					ignoredLowFeeTickets[*hash] = userVotingConfig[addr].MultiSigAddress
				}
			}
			break
		}
	}

	log.Infof("tickets loaded -- addedLowFee %v ignoredLowFee %v normalFee %v "+
		"live %v total %v", len(ctx.AddedLowFeeTicketsMSA),
		len(ignoredLowFeeTickets), normalFee, len(liveTickets),
		len(tickets))

	return ignoredLowFeeTickets, liveTickets, nil
}
