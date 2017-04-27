package main

import (
	"fmt"
	"io/ioutil"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrstakepool/backend/stakepoold/userdata"
	"github.com/decred/dcrutil"
)

var requiredChainServerAPI = semver{major: 3, minor: 0, patch: 0}
var requiredWalletAPI = semver{major: 3, minor: 0, patch: 0}

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

func walletFetchUserTickets(ctx *appContext) map[chainhash.Hash]string {
	// This is suboptimal to copy and needs fixing.
	users := make(map[string]userdata.UserVotingConfig)
	ctx.RLock()
	for k, v := range ctx.userVotingConfig {
		users[k] = v
	}
	ctx.RUnlock()

	userTickets := make(map[chainhash.Hash]string)

	type promise struct {
		dcrrpcclient.FutureStakePoolUserInfoResult
		msa string
	}
	promises := make([]promise, 0, len(users))
	for msa, v := range users {
		addr, err := dcrutil.DecodeAddress(msa, ctx.params)
		if err != nil {
			log.Infof("Could not decode multisig address %v for %v: %v",
				msa, v.Userid, err)
			continue
		}

		promises = append(promises, promise{
			ctx.walletConnection.StakePoolUserInfoAsync(addr), msa})
	}

	var (
		ticketcount, usercount int
	)

	for _, p := range promises {
		spui, err := p.Receive()
		if err != nil {
			log.Errorf("unable to fetch tickets for user %v multisigaddr %v: %v",
				users[p.msa].Userid, p.msa, err)
			continue
		}

		if spui == nil || len(spui.Tickets) <= 0 {
			continue
		}

		for _, ticket := range spui.Tickets {
			if ticket.Status != "live" {
				continue
			}

			hash, err := chainhash.NewHashFromStr(ticket.Ticket)
			if err != nil {
				log.Infof("invalid ticket %v", err)
				continue
			}

			userTickets[*hash] = users[p.msa].MultiSigAddress
			ticketcount++
		}
		usercount++
	}

	ticketNoun := pickNoun(ticketcount, "ticket", "tickets")
	userNoun := pickNoun(usercount, "user", "users")

	log.Infof("loaded %v %v for %v %v",
		ticketcount, ticketNoun, usercount, userNoun)

	return userTickets
}
