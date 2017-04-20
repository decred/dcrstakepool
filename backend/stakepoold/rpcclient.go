package main

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
)

var requiredChainServerAPI = semver{major: 2, minor: 0, patch: 0}
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

func nodeSendVote(ctx *appContext, hexTx string) (*chainhash.Hash, error) {
	buf, err := hex.DecodeString(hexTx)
	if err != nil {
		log.Errorf("DecodeString failed: %v", err)
		return nil, err
	}
	newTx := wire.NewMsgTx()
	err = newTx.FromBytes(buf)
	if err != nil {
		return nil, err
	}

	return ctx.nodeConnection.SendRawTransaction(newTx, false)
}

func walletCreateVote(ctx *appContext, blockHash *chainhash.Hash, blockHeight int64, ticketHash *chainhash.Hash, msa string) (string, error) {
	// look up the voting config for this user's ticket
	userVotingConfig, ok := ctx.userVotingConfig[msa]
	if !ok {
		log.Errorf("Unknown multisig address %s. Cannot create vote.", msa)
		// do not return; vote with defaults
	}
	votebitsToUse := userVotingConfig.VoteBits
	votebitsVersionToUse := userVotingConfig.VoteBitsVersion

	// if the user's voting config has a vote version that is different from our
	// global vote version that we plucked from dcrwallet walletinfo then just
	// use the default votebits of 1
	if votebitsVersionToUse != ctx.votingConfig.VoteVersion {
		votebitsToUse = ctx.votingConfig.VoteBits
		log.Infof("userid %v multisigaddress %v vote version mismatch user %v "+
			"stakepoold %v using votebits %d",
			userVotingConfig.Userid,
			userVotingConfig.MultiSigAddress, votebitsVersionToUse,
			ctx.votingConfig.VoteVersion, votebitsToUse)
	}

	log.Infof("calling GenerateVote with blockHash %v blockHeight %v ticketHash %v votebitsToUse %v voteBitsExt %v",
		blockHash, blockHeight, ticketHash, votebitsToUse, ctx.votingConfig.VoteBitsExtended)
	res, err := ctx.walletConnection.GenerateVote(blockHash, blockHeight, ticketHash, votebitsToUse, ctx.votingConfig.VoteBitsExtended)
	if err != nil {
		return "", err
	}

	return res.Hex, nil
}

func walletSendVote(ctx *appContext, hexTx string) (*chainhash.Hash, error) {
	buf, err := hex.DecodeString(hexTx)
	if err != nil {
		log.Errorf("DecodeString failed: %v", err)
		return nil, err
	}
	newTx := wire.NewMsgTx()
	err = newTx.FromBytes(buf)
	if err != nil {
		return nil, err
	}

	return ctx.walletConnection.SendRawTransaction(newTx, false)
}

func walletFetchUserTickets(ctx *appContext) map[string]UserTickets {
	userTickets := map[string]UserTickets{}

	ticketcount := 0
	usercount := 0
	for msa := range ctx.userVotingConfig {
		addr, err := dcrutil.DecodeAddress(msa, ctx.params)
		if err != nil {
			log.Infof("unable to decode multisig address: %v", err)
			continue
		}

		spui, err := ctx.walletConnection.StakePoolUserInfo(addr)
		if err != nil {
			log.Errorf("unable to fetch tickets for userid %v multisigaddr %v: %v",
				ctx.userVotingConfig[msa].Userid, msa, err)
			continue
		}
		if spui != nil && len(spui.Tickets) > 0 {
			tickets := make([]*chainhash.Hash, 0)
			for _, ticket := range spui.Tickets {
				switch ticket.Status {
				case "live":
					hash, err := chainhash.NewHashFromStr(ticket.Ticket)
					if err != nil {
						log.Infof("invalid ticket %v", err)
					} else {
						tickets = append(tickets, hash)
					}
				}
			}
			if len(tickets) > 0 {
				userTickets[msa] = UserTickets{
					Userid:          ctx.userVotingConfig[msa].Userid,
					MultiSigAddress: ctx.userVotingConfig[msa].MultiSigAddress,
					Tickets:         tickets,
				}
				usercount++
				ticketcount = ticketcount + len(tickets)
			}
		}
	}

	ticketNoun := pickNoun(ticketcount, "ticket", "tickets")
	userNoun := pickNoun(usercount, "user", "users")

	log.Infof("loaded %v %v for %v %v",
		ticketcount, ticketNoun, usercount, userNoun)

	return userTickets
}
