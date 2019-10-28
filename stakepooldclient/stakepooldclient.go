package stakepooldclient

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	pb "github.com/decred/dcrstakepool/backend/stakepoold/rpc/stakepoolrpc"
	"github.com/decred/dcrstakepool/helpers"
	"github.com/decred/dcrstakepool/models"
	"golang.org/x/net/context"
)

var (
	requiredStakepooldAPI = semver{major: 9, minor: 0, patch: 0}

	// cacheTimerStakeInfo is the duration of time after which to
	// access the wallet and update the stake information instead
	// of returning cached stake information.
	cacheTimerStakeInfo = 5 * time.Minute

	// defaultAccountName is the account name for the default wallet
	// account as a string.
	defaultAccountName = "default"
)

// StakepooldManager coordinates the communication between dcrstakepool and
// multiple instances of stakepoold. All interaction with stakepoold should
// be through StakepooldManager.
type StakepooldManager struct {
	grpcConnections []*grpc.ClientConn
	// cachedStakeInfo is cached information about the voting service wallet.
	// This is required because of the time it takes to compute the stake
	// information. The included timer is used so that new stake information is
	// only queried for if 5 minutes or more has passed. The mutex is used to
	// allow concurrent access to the stake information if less than five
	// minutes has passed.
	cachedStakeInfo      *pb.GetStakeInfoResponse
	cachedStakeInfoTimer time.Time
	cachedStakeInfoMutex sync.Mutex
}

// ConnectStakepooldGRPC establishes a gRPC connection with all provided
// stakepoold hosts. Returns an error if any host cannot be contacted,
// has the wrong RPC version, or is otherwise mis-configured.
func ConnectStakepooldGRPC(stakepooldHosts []string, stakepooldCerts []string) (*StakepooldManager, error) {
	conns := make([]*grpc.ClientConn, len(stakepooldHosts))

	for serverID := range stakepooldHosts {
		log.Infof("Attempting to connect to stakepoold gRPC %s using "+
			"certificate located in %s", stakepooldHosts[serverID],
			stakepooldCerts[serverID])
		creds, err := credentials.NewClientTLSFromFile(stakepooldCerts[serverID], "localhost")
		if err != nil {
			return nil, err
		}
		conn, err := grpc.Dial(stakepooldHosts[serverID], grpc.WithTransportCredentials(creds))
		if err != nil {
			return nil, err
		}
		c := pb.NewVersionServiceClient(conn)

		versionResponse, err := c.Version(context.Background(), &pb.VersionRequest{})
		if err != nil {
			return nil, err
		}

		var semverResponse = semver{
			major: versionResponse.Major,
			minor: versionResponse.Minor,
			patch: versionResponse.Patch,
		}

		if !semverCompatible(requiredStakepooldAPI, semverResponse) {
			return nil, fmt.Errorf("Stakepoold gRPC server %s does not have "+
				"a compatible API version. Advertises %v but require %v",
				stakepooldHosts[serverID], versionResponse, requiredStakepooldAPI)
		}

		log.Infof("Established connection to gRPC server %s",
			stakepooldHosts[serverID])
		conns[serverID] = conn
	}

	return &StakepooldManager{grpcConnections: conns}, nil
}

// connected uses WalletInfo RPC to check that all stakepoold and
// dcrwallet instances are currently online and reachable. Also
// checks that dcrwallet is unlocked and connected to dcrd. This
// should be performed before any write operations.
func (s *StakepooldManager) connected() error {
	responses, err := s.WalletInfo()
	if err != nil {
		return err
	}

	for i, resp := range responses {
		if !resp.DaemonConnected {
			return fmt.Errorf("wallet[%d] is not connected to dcrd", i)
		}
		if !resp.Unlocked {
			return fmt.Errorf("wallet[%d] is not unlocked", i)
		}
	}

	return nil
}

// GetAddedLowFeeTickets performs gRPC GetAddedLowFeeTickets
// requests against all stakepoold instances and returns the first result fetched
// without errors. Returns an error if all RPC requests fail.
func (s *StakepooldManager) GetAddedLowFeeTickets() (map[chainhash.Hash]string, error) {
	for _, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		resp, err := client.GetAddedLowFeeTickets(context.Background(), &pb.GetAddedLowFeeTicketsRequest{})
		if err != nil {
			log.Warnf("GetAddedLowFeeTickets RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			continue
		}

		addedLowFeeTickets := processTicketsResponse(resp.Tickets)
		log.Infof("stakepoold %s reports %d AddedLowFee tickets", conn.Target(), len(addedLowFeeTickets))
		return addedLowFeeTickets, err
	}

	// All RPC requests failed
	return nil, errors.New("GetAddedLowFeeTickets RPC failed on all stakepoold instances")
}

// GetIgnoredLowFeeTickets performs gRPC GetIgnoredLowFeeTickets
// requests against all stakepoold instances and returns the first result fetched
// without errors. Returns an error if all RPC requests fail.
func (s *StakepooldManager) GetIgnoredLowFeeTickets() (map[chainhash.Hash]string, error) {
	for _, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		resp, err := client.GetIgnoredLowFeeTickets(context.Background(), &pb.GetIgnoredLowFeeTicketsRequest{})
		if err != nil {
			log.Warnf("GetIgnoredLowFeeTickets RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			continue
		}

		ignoredLowFeeTickets := processTicketsResponse(resp.Tickets)
		log.Infof("stakepoold %s reports %d IgnoredLowFee tickets", conn.Target(), len(ignoredLowFeeTickets))
		return ignoredLowFeeTickets, nil
	}

	// All RPC requests failed
	return nil, errors.New("GetIgnoredLowFeeTickets RPC failed on all stakepoold instances")
}

// GetLiveTickets performs gRPC GetLiveTickets
// requests against all stakepoold instances and returns the first result fetched
// without errors. Returns an error if all RPC requests fail.
func (s *StakepooldManager) GetLiveTickets() (map[chainhash.Hash]string, error) {
	for _, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		resp, err := client.GetLiveTickets(context.Background(), &pb.GetLiveTicketsRequest{})
		if err != nil {
			log.Warnf("GetLiveTickets RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			continue
		}

		liveTickets := processTicketsResponse(resp.Tickets)
		log.Infof("stakepoold %s reports %d Live Tickets", conn.Target(), len(liveTickets))
		return liveTickets, nil
	}

	// All RPC requests failed
	return nil, errors.New("GetLiveTickets RPC failed on all stakepoold instances")
}

func processTicketsResponse(tickets []*pb.Ticket) map[chainhash.Hash]string {
	processedTickets := make(map[chainhash.Hash]string)
	for _, ticket := range tickets {
		hash, err := chainhash.NewHash(ticket.Hash)
		if err != nil {
			log.Warnf("NewHash failed for %v: %v", ticket.Hash, err)
			continue
		}
		processedTickets[*hash] = ticket.Address
	}

	return processedTickets
}

// SetAddedLowFeeTickets calls SetAddedLowFeeTickets RPC on all stakepoold instances. It stops
// executing and returns an error if any RPC call fails
func (s *StakepooldManager) SetAddedLowFeeTickets(dbTickets []models.LowFeeTicket) error {
	if err := s.connected(); err != nil {
		log.Errorf("SetAddedLowFeeTickets: stakepoold failed connectivity check: %v", err)
		return err
	}

	tickets := make([]*pb.Ticket, 0, len(dbTickets))
	for _, ticket := range dbTickets {
		hash, err := chainhash.NewHashFromStr(ticket.TicketHash)
		if err != nil {
			log.Warnf("NewHashFromStr failed for %v: %v", ticket.TicketHash, err)
			continue
		}
		tickets = append(tickets, &pb.Ticket{
			Address: ticket.TicketAddress,
			Hash:    hash.CloneBytes(),
		})
	}

	setAddedTicketsReq := &pb.SetAddedLowFeeTicketsRequest{
		Tickets: tickets,
	}
	for _, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		_, err := client.SetAddedLowFeeTickets(context.Background(),
			setAddedTicketsReq)
		if err != nil {
			log.Errorf("SetAddedLowFeeTickets RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			return err
		}
	}

	log.Info("SetAddedLowFeeTickets successful on all stakepoold instances")
	return nil
}

// CreateMultisig performs gRPC CreateMultisig on all servers. It stops
// executing and returns an error if any RPC call fails. It will
// also return an error if any of the responses are different. This
// should be considered fatal, as it indicates that a voting wallet is
// misconfigured
func (s *StakepooldManager) CreateMultisig(address []string) (*pb.CreateMultisigResponse, error) {
	if err := s.connected(); err != nil {
		log.Errorf("CreateMultisig: stakepoold failed connectivity check: %v", err)
		return nil, err
	}

	respPerServer := make([]*pb.CreateMultisigResponse, len(s.grpcConnections))
	request := &pb.CreateMultisigRequest{
		Address: address,
	}

	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)

		resp, err := client.CreateMultisig(context.Background(), request)
		if err != nil {
			log.Errorf("CreateMultisig: CreateMultisig RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			return nil, err
		}
		respPerServer[i] = resp
	}

	for i := 0; i < len(s.grpcConnections)-1; i++ {
		if respPerServer[i].RedeemScript != respPerServer[i+1].RedeemScript {
			log.Errorf("CreateMultisig: nonequiv failure on servers "+
				"%v, %v (%v != %v)", i, i+1, respPerServer[i].RedeemScript, respPerServer[i+1].RedeemScript)
			return nil, fmt.Errorf("non equivalent redeem script returned")
		}
	}

	return respPerServer[0], nil
}

// SyncAll ensures that the wallet servers are all in sync with each
// other in terms of tickets, redeem scripts and address indexes.
func (s *StakepooldManager) SyncAll(multiSigScripts []models.User, maxUsers int64) error {
	if err := s.connected(); err != nil {
		log.Errorf("SyncAll: stakepoold failed connectivity check: %v", err)
		return err
	}

	// Set watched address indexes to maxUsers so all generated ticket
	// addresses show as 'ismine'.
	err := s.syncWatchedAddresses(defaultAccountName, helpers.ExternalBranch, maxUsers)
	if err != nil {
		return err
	}

	// Synchronize the redeem scripts so all of our voting wallets can
	// vote on all known tickets.
	err = s.syncScripts(multiSigScripts)
	if err != nil {
		return err
	}

	// Synchronize the tickets so all voting wallets are aware of all tickets.
	err = s.syncTickets()
	if err != nil {
		return err
	}

	return nil
}

// syncWatchedAddresses calls AccountSyncAddressIndex RPC on all stakepoold instances. It stops
// executing and returns an error if any RPC call fails
func (s *StakepooldManager) syncWatchedAddresses(accountName string, branch uint32, maxUsers int64) error {
	log.Info("syncWatchedAddresses: Attempting to synchronise watched addresses across voting wallets")

	request := &pb.AccountSyncAddressIndexRequest{
		Account: accountName,
		Branch:  branch,
		Index:   maxUsers,
	}

	for _, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)

		_, err := client.AccountSyncAddressIndex(context.Background(), request)
		if err != nil {
			log.Errorf("syncWatchedAddresses: AccountSyncAddressIndex RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			return err
		}
	}

	log.Infof("syncWatchedAddresses: Complete")

	return nil
}

// syncScripts collates all known redeem scripts from the database and from
// each stakepoold instance. It then iterates over each stakepoold instance
// and imports any missing scripts. Returns an error immediately if any RPC
// call fails.
func (s *StakepooldManager) syncScripts(multiSigScripts []models.User) error {
	type ScriptHeight struct {
		Script []byte
		Height int
	}

	log.Info("syncScripts: Attempting to synchronise redeem scripts across voting wallets")

	// Get all scripts from db
	allRedeemScripts := make(map[chainhash.Hash]*ScriptHeight)

	for _, v := range multiSigScripts {
		byteScript, err := hex.DecodeString(v.MultiSigScript)
		if err != nil {
			log.Errorf("syncScripts: Failed to decode script %s due to err: %v", v.MultiSigScript, err)
			return err
		}
		allRedeemScripts[chainhash.HashH(byteScript)] = &ScriptHeight{byteScript, int(v.HeightRegistered)}
	}

	log.Infof("syncScripts: %d scripts found in the db", len(allRedeemScripts))

	// Fetch the scripts from each server.
	redeemScriptsPerServer := make([]map[chainhash.Hash]*ScriptHeight,
		len(s.grpcConnections))

	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)

		resp, err := client.ListScripts(context.Background(), &pb.ListScriptsRequest{})
		if err != nil {
			return err
		}

		redeemScriptsPerServer[i] = make(map[chainhash.Hash]*ScriptHeight)
		for _, script := range resp.Scripts {
			redeemScriptsPerServer[i][chainhash.HashH(script)] = &ScriptHeight{script, 0}
			_, ok := allRedeemScripts[chainhash.HashH(script)]
			if !ok {
				allRedeemScripts[chainhash.HashH(script)] = &ScriptHeight{script, 0}
			}
		}

		log.Infof("syncScripts: stakepoold %s reports %d scripts", conn.Target(), len(redeemScriptsPerServer[i]))
	}

	// Check each server has every script
	for i, conn := range s.grpcConnections {
		// Because we are adding historic scripts, we need to perform a rescan.
		// Need to find the earliest imported script height so we know where scan from.
		missingScripts := make([][]byte, 0)
		earliestHeight := -1
		for k, v := range allRedeemScripts {
			_, ok := redeemScriptsPerServer[i][k]
			if !ok {
				// Script is missing from server
				missingScripts = append(missingScripts, v.Script)
				if earliestHeight == -1 || earliestHeight > v.Height {
					earliestHeight = v.Height
				}
			}
		}

		// Add missing scripts to the server if there are any
		if len(missingScripts) > 0 {
			log.Infof("syncScripts: stakepoold %s was missing %d redeem scripts. Importing now...", conn.Target(), len(missingScripts))
			client := pb.NewStakepooldServiceClient(conn)

			request := &pb.ImportMissingScriptsRequest{
				Scripts:      missingScripts,
				RescanHeight: int64(earliestHeight),
			}

			_, err := client.ImportMissingScripts(context.Background(), request)
			if err != nil {
				return err
			}
		}
	}

	log.Infof("syncScripts: Complete")

	return nil
}

// syncTickets retrieves all owned tickets from each stakepoold instance, and then
// ensures that any missing tickets are added to the wallets which are missing them.
// Returns an error immediately if any RPC call fails.
func (s *StakepooldManager) syncTickets() error {
	ticketsPerServer := make([]map[string]struct{}, len(s.grpcConnections))
	allTickets := make(map[string]struct{})

	log.Infof("syncTickets: Attempting to synchronise tickets across voting wallets")

	request := &pb.GetTicketsRequest{
		IncludeImmature: true,
	}

	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)

		resp, err := client.GetTickets(context.Background(), request)
		if err != nil {
			log.Errorf("syncTickets: GetTickets RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			return err
		}

		ticketsPerServer[i] = make(map[string]struct{})
		for _, ticketHash := range resp.Tickets {
			ticketsPerServer[i][string(ticketHash)] = struct{}{}
			allTickets[string(ticketHash)] = struct{}{}
		}

		log.Infof("syncTickets: stakepoold %s reports %d tickets", conn.Target(), len(ticketsPerServer[i]))
	}

	for i, conn := range s.grpcConnections {
		for ticketHash := range allTickets {
			_, ok := ticketsPerServer[i][ticketHash]
			if !ok {
				log.Infof("syncTickets: stakepoold %s is missing ticket %v", conn.Target(), ticketHash)

				client := pb.NewStakepooldServiceClient(conn)
				request := &pb.AddMissingTicketRequest{
					Hash: []byte(ticketHash),
				}
				_, err := client.AddMissingTicket(context.Background(), request)
				if err != nil {
					log.Errorf("syncTickets: AddMissingTicket RPC failed on stakepoold instance %s: %v", conn.Target(), err)
					return err
				}
			}
		}
	}

	log.Infof("syncTickets: Complete")

	return nil
}

// StakePoolUserInfo performs gRPC StakePoolUserInfo. It sends requests to
// instances of stakepoold and returns the first successful response. Returns
// an error if RPC to all instances of stakepoold fail
func (s *StakepooldManager) StakePoolUserInfo(multiSigAddress string) (*pb.StakePoolUserInfoResponse, error) {
	request := &pb.StakePoolUserInfoRequest{
		MultiSigAddress: multiSigAddress,
	}

	for _, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		response, err := client.StakePoolUserInfo(context.Background(), request)
		if err != nil {
			log.Warnf("StakePoolUserInfo RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			continue
		}

		return response, nil
	}

	// All RPC requests failed
	return nil, errors.New("StakePoolUserInfo RPC failed on all stakepoold instances")
}

// SetUserVotingPrefs performs gRPC SetUserVotingPrefs. It stops
// executing and returns an error if any RPC call fails
func (s *StakepooldManager) SetUserVotingPrefs(dbUsers map[int64]*models.User) error {
	if err := s.connected(); err != nil {
		log.Errorf("SetUserVotingPrefs: stakepoold failed connectivity check: %v", err)
		return err
	}

	users := make([]*pb.UserVotingConfigEntry, 0, len(dbUsers))
	for userid, data := range dbUsers {
		users = append(users, &pb.UserVotingConfigEntry{
			UserId:          userid,
			MultiSigAddress: data.MultiSigAddress,
			VoteBits:        data.VoteBits,
			VoteBitsVersion: data.VoteBitsVersion,
		})
	}

	setVotingConfigReq := &pb.SetUserVotingPrefsRequest{
		UserVotingConfig: users,
	}

	for _, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		_, err := client.SetUserVotingPrefs(context.Background(),
			setVotingConfigReq)
		if err != nil {
			log.Errorf("SetUserVotingPrefs RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			return err
		}
	}

	log.Info("SetUserVotingPrefs successful on all stakepoold instances")
	return nil
}

// WalletInfo calls WalletInfo RPC on all stakepoold instances. It stops
// executing and returns an error if any RPC call fails
func (s *StakepooldManager) WalletInfo() ([]*pb.WalletInfoResponse, error) {
	responses := make([]*pb.WalletInfoResponse, len(s.grpcConnections))

	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		resp, err := client.WalletInfo(context.Background(), &pb.WalletInfoRequest{})
		if err != nil {
			log.Errorf("WalletInfo RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			return nil, err
		}
		responses[i] = resp
	}

	return responses, nil
}

// ValidateAddress calls ValidateAddress RPC on all stakepoold servers.
// Returns an error if responses are not the same from all stakepoold instances.
func (s *StakepooldManager) ValidateAddress(addr dcrutil.Address) (*pb.ValidateAddressResponse, error) {
	responses := make(map[int]*pb.ValidateAddressResponse)

	req := &pb.ValidateAddressRequest{
		Address: addr.Address(),
	}

	// Get ValidateAddress response from all wallets
	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		resp, err := client.ValidateAddress(context.Background(), req)
		if err != nil {
			log.Errorf("ValidateAddress RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			return nil, err
		}
		responses[i] = resp
	}

	// Ensure responses are identical
	var lastResponse *pb.ValidateAddressResponse
	var lastServer int
	firstrun := true
	for k, v := range responses {
		if firstrun {
			firstrun = false
			lastResponse = v
		}

		if v.IsMine != lastResponse.IsMine ||
			v.PubKeyAddr != lastResponse.PubKeyAddr {
			vErr := fmt.Errorf("wallets %d and %d have different ValidateAddress responses",
				k, lastServer)
			return nil, vErr
		}

		lastServer = k
	}

	return lastResponse, nil
}

// ImportNewScript calls ImportNewScript RPC on all stakepoold instances. It stops
// executing and returns an error if any RPC call fails.
// Because this is a new script, no rescan is necessary.
func (s *StakepooldManager) ImportNewScript(script []byte) (heightImported int64, err error) {
	if err := s.connected(); err != nil {
		log.Errorf("ImportNewScript: stakepoold failed connectivity check: %v", err)
		return -1, err
	}

	req := &pb.ImportNewScriptRequest{
		Script: script,
	}

	for _, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		resp, err := client.ImportNewScript(context.Background(), req)
		if err != nil {
			log.Errorf("ImportNewScript RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			return -1, err
		}
		heightImported = resp.HeightImported
	}

	log.Info("ImportNewScript successful on all stakepoold instances")
	return heightImported, err
}

// BackendStatus provides a summary of a single back-end server
type BackendStatus struct {
	Host      string
	RPCStatus string
	*WalletStatus
}

type WalletStatus struct {
	DaemonConnected bool
	VoteVersion     uint32
	Unlocked        bool
	Voting          bool
}

// BackendStatus uses the state of each RPC connection and the
// WalletInfo RPC to return a summary of the state of each
// connected back-end server.
func (s *StakepooldManager) BackendStatus() []BackendStatus {
	stakepooldPageInfo := make([]BackendStatus, len(s.grpcConnections))

	for i, conn := range s.grpcConnections {
		stakepooldPageInfo[i].Host = conn.Target()
		stakepooldPageInfo[i].RPCStatus = "Unknown"
		state := conn.GetState()
		switch state {
		case connectivity.Idle:
			stakepooldPageInfo[i].RPCStatus = "Idle"
		case connectivity.Shutdown:
			stakepooldPageInfo[i].RPCStatus = "Shutdown"
		case connectivity.Ready:
			stakepooldPageInfo[i].RPCStatus = "Ready"
		case connectivity.Connecting:
			stakepooldPageInfo[i].RPCStatus = "Connecting"
		case connectivity.TransientFailure:
			stakepooldPageInfo[i].RPCStatus = "TransientFailure"
		}

		client := pb.NewStakepooldServiceClient(conn)
		req := &pb.WalletInfoRequest{}
		resp, err := client.WalletInfo(context.Background(), req)
		if err != nil {
			log.Warnf("BackendStatus: WalletInfo RPC failed on stakepoold instance %s: %v", conn.Target(), err)
		} else {
			stakepooldPageInfo[i].WalletStatus = &WalletStatus{
				DaemonConnected: resp.DaemonConnected,
				VoteVersion:     resp.VoteVersion,
				Unlocked:        resp.Unlocked,
				Voting:          resp.Voting,
			}
		}
	}

	return stakepooldPageInfo
}

// GetStakeInfo returns cached stake info if within cachedStakeInfoTimer limit
// from last cache. Otherwise it calls GetStakeInfo RPC on all stakepoold
// instances until receiving a response. The response is cached. Returns an
// error if all RPC calls fail.
func (s *StakepooldManager) GetStakeInfo() (*pb.GetStakeInfoResponse, error) {
	defer s.cachedStakeInfoMutex.Unlock()
	s.cachedStakeInfoMutex.Lock()

	now := time.Now()
	if s.cachedStakeInfoTimer.After(now) {
		return s.cachedStakeInfo, nil
	}

	for _, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		resp, err := client.GetStakeInfo(context.Background(), &pb.GetStakeInfoRequest{})
		if err != nil {
			log.Warnf("GetStakeInfo RPC failed on stakepoold instance %s: %v", conn.Target(), err)
			continue
		}
		s.cachedStakeInfo = resp
		s.cachedStakeInfoTimer = now.Add(cacheTimerStakeInfo)
		return resp, nil
	}
	return nil, errors.New("GetStakeInfo RPC failed on all stakepoold instances")
}

// CrossCheckColdWalletExtPubs calls GetColdWalletExtPub RPC on all stakepoold
// instances and compares the returned `coldwalletextpub` value against the
// value set in dcrstakepool's config.
// Returns an error if an RPC call to any of the backend clients errors or
// if any returned `coldwalletextpub` value is not the same as dcrstakepool's.
func (s *StakepooldManager) CrossCheckColdWalletExtPubs(dcrstakepoolColdWalletExtPub string) error {
	for _, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		stakepooldResp, err := client.GetColdWalletExtPub(context.Background(), &pb.GetColdWalletExtPubRequest{})
		if err != nil {
			return fmt.Errorf("GetColdWalletExtPub RPC failed on stakepoold instance %s: %v", conn.Target(), err)
		}
		if stakepooldResp.ColdWalletExtPub != dcrstakepoolColdWalletExtPub {
			return fmt.Errorf("coldwalletextpub incorrectly configured on stakepoold instance %s", conn.Target())
		}
	}
	return nil
}
