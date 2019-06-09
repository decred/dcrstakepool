package stakepooldclient

import (
	"errors"
	"fmt"
	"github.com/decred/dcrd/dcrutil"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"

	"github.com/decred/dcrd/chaincfg/chainhash"
	pb "github.com/decred/dcrstakepool/backend/stakepoold/rpc/stakepoolrpc"
	"github.com/decred/dcrstakepool/models"
	"golang.org/x/net/context"
)

var requiredStakepooldAPI = semver{major: 4, minor: 0, patch: 0}

type StakepooldManager struct {
	grpcConnections []*grpc.ClientConn
}

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

		versionRequest := &pb.VersionRequest{}
		versionResponse, err := c.Version(context.Background(), versionRequest)
		if err != nil {
			return nil, err
		}

		var semverResponse = semver{
			major: versionResponse.Major,
			minor: versionResponse.Minor,
			patch: versionResponse.Patch,
		}

		if !semverCompatible(requiredStakepooldAPI, semverResponse) {
			return nil, fmt.Errorf("Stakepoold gRPC server does not have "+
				"a compatible API version. Advertises %v but require %v",
				versionResponse, requiredStakepooldAPI)
		}

		log.Infof("Established connection to gRPC server %s",
			stakepooldHosts[serverID])
		conns[serverID] = conn
	}

	return &StakepooldManager{conns}, nil
}

// GetAddedLowFeeTickets performs gRPC GetAddedLowFeeTickets
// requests against all stakepoold instances and returns the first result fetched
// without errors. Returns an error if all RPC requests fail.
func (s *StakepooldManager) GetAddedLowFeeTickets() (map[chainhash.Hash]string, error) {
	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		resp, err := client.GetAddedLowFeeTickets(context.Background(), &pb.GetAddedLowFeeTicketsRequest{})
		if err != nil {
			log.Warnf("GetAddedLowFeeTickets RPC failed on stakepoold instance %d: %v", i, err)
			continue
		}

		addedLowFeeTickets := processTicketsResponse(resp.Tickets)
		log.Infof("stakepoold %d reports %d AddedLowFee tickets", i, len(addedLowFeeTickets))
		return addedLowFeeTickets, err
	}

	// All RPC requests failed
	return nil, errors.New("GetAddedLowFeeTickets RPC failed on all stakepoold instances")
}

// GetIgnoredLowFeeTickets performs gRPC GetIgnoredLowFeeTickets
// requests against all stakepoold instances and returns the first result fetched
// without errors. Returns an error if all RPC requests fail.
func (s *StakepooldManager) GetIgnoredLowFeeTickets() (map[chainhash.Hash]string, error) {
	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		resp, err := client.GetIgnoredLowFeeTickets(context.Background(), &pb.GetIgnoredLowFeeTicketsRequest{})
		if err != nil {
			log.Warnf("GetIgnoredLowFeeTickets RPC failed on stakepoold instance %d: %v", i, err)
			continue
		}

		ignoredLowFeeTickets := processTicketsResponse(resp.Tickets)
		log.Infof("stakepoold %d reports %d IgnoredLowFee tickets", i, len(ignoredLowFeeTickets))
		return ignoredLowFeeTickets, nil
	}

	// All RPC requests failed
	return nil, errors.New("GetIgnoredLowFeeTickets RPC failed on all stakepoold instances")
}

// GetLiveTickets performs gRPC GetLiveTickets
// requests against all stakepoold instances and returns the first result fetched
// without errors. Returns an error if all RPC requests fail.
func (s *StakepooldManager) GetLiveTickets() (map[chainhash.Hash]string, error) {
	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		resp, err := client.GetLiveTickets(context.Background(), &pb.GetLiveTicketsRequest{})
		if err != nil {
			log.Warnf("GetLiveTickets RPC failed on stakepoold instance %d: %v", i, err)
			continue
		}

		liveTickets := processTicketsResponse(resp.Tickets)
		log.Infof("stakepoold %d reports %d Live Tickets", i, len(liveTickets))
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

// SetAddedLowFeeTickets performs gRPC SetAddedLowFeeTickets. It stops
// executing and returns an error if any RPC call fails
func (s *StakepooldManager) SetAddedLowFeeTickets(dbTickets []models.LowFeeTicket) error {
	var tickets []*pb.Ticket
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

	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		setAddedTicketsReq := &pb.SetAddedLowFeeTicketsRequest{
			Tickets: tickets,
		}
		_, err := client.SetAddedLowFeeTickets(context.Background(),
			setAddedTicketsReq)
		if err != nil {
			log.Errorf("SetAddedLowFeeTickets RPC failed on stakepoold instance %d: %v", i, err)
			return err
		}
	}

	log.Info("SetAddedLowFeeTickets successful on all stakepoold instances")
	return nil
}

// StakePoolUserInfo performs gRPC StakePoolUserInfo. It sends requests to
// instances of stakepoold and returns the first successful response. Returns
// an error if RPC to all instances of stakepoold fail
func (s *StakepooldManager) StakePoolUserInfo(multiSigAddress string) (*pb.StakePoolUserInfoResponse, error) {
	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		request := &pb.StakePoolUserInfoRequest{
			MultiSigAddress: multiSigAddress,
		}
		response, err := client.StakePoolUserInfo(context.Background(), request)
		if err != nil {
			log.Warnf("StakePoolUserInfo RPC failed on stakepoold instance %d: %v", i, err)
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
	var users []*pb.UserVotingConfigEntry
	for userid, data := range dbUsers {
		users = append(users, &pb.UserVotingConfigEntry{
			UserId:          userid,
			MultiSigAddress: data.MultiSigAddress,
			VoteBits:        data.VoteBits,
			VoteBitsVersion: data.VoteBitsVersion,
		})
	}

	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		setVotingConfigReq := &pb.SetUserVotingPrefsRequest{
			UserVotingConfig: users,
		}
		_, err := client.SetUserVotingPrefs(context.Background(),
			setVotingConfigReq)
		if err != nil {
			log.Errorf("SetUserVotingPrefs RPC failed on stakepoold instance %d: %v", i, err)
			return err
		}
	}

	log.Info("SetUserVotingPrefs successful on all stakepoold instances")
	return nil
}

// VoteVersion returns a consistent vote version between all wallets
// or an error indicating a mismatch
func (s *StakepooldManager) VoteVersion() (uint32, error) {
	walletVoteVersions := make(map[int]uint32)

	// Get vote version from all wallets
	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		req := &pb.WalletInfoRequest{}
		wvv, err := client.WalletInfo(context.Background(), req)
		if err != nil {
			log.Errorf("WalletInfo RPC failed on stakepoold instance %d: %v", i, err)
			return 0, err
		}
		walletVoteVersions[i] = wvv.VoteVersion
	}

	// Ensure vote version matches on all wallets
	lastVersion := uint32(0)
	lastServer := 0
	firstrun := true
	for k, v := range walletVoteVersions {
		if firstrun {
			firstrun = false
			lastVersion = v
		}

		if v != lastVersion {
			vErr := fmt.Errorf("wallets %d and %d have mismatched vote versions",
				k, lastServer)
			return 0, vErr
		}

		lastServer = k
	}

	return lastVersion, nil
}

// ValidateAddress calls ValidateAddress RPC on all stakepoold servers.
// Returns an error if responses are not the same from all stakepoold instances.
func (s *StakepooldManager) ValidateAddress(addr dcrutil.Address) (*pb.ValidateAddressResponse, error) {
	responses := make(map[int]*pb.ValidateAddressResponse)

	// Get ValidateAddress response from all wallets
	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		req := &pb.ValidateAddressRequest{
			Address: addr.EncodeAddress(),
		}
		resp, err := client.ValidateAddress(context.Background(), req)
		if err != nil {
			log.Errorf("ValidateAddress RPC failed on stakepoold instance %d: %v", i, err)
			return nil, err
		}
		responses[i] = resp
	}

	// Ensure responses are identical
	var lastResponse *pb.ValidateAddressResponse
	lastServer := 0
	firstrun := true
	for k, v := range responses {
		if firstrun {
			firstrun = false
			lastResponse = v
		}

		if v.IsMine != lastResponse.IsMine ||
			v.PubKeyAddr != lastResponse.PubKeyAddr {
			vErr := fmt.Errorf("wallets %d and %d have different ValideAddress responses",
				k, lastServer)
			return nil, vErr
		}

		lastServer = k
	}

	return lastResponse, nil
}

// ImportScript calls ImportScript RPC on all stakepoold instances. It stops
// executing and returns an error if any RPC call fails
func (s *StakepooldManager) ImportScript(script []byte) (heightImported int64, err error) {
	for i, conn := range s.grpcConnections {
		client := pb.NewStakepooldServiceClient(conn)
		req := &pb.ImportScriptRequest{
			Script: script,
		}
		resp, err := client.ImportScript(context.Background(), req)
		if err != nil {
			log.Errorf("ImportScript RPC failed on stakepoold instance %d: %v", i, err)
			return -1, err
		}
		heightImported = resp.HeightImported
	}

	log.Info("ImportScript successful on all stakepoold instances")
	return heightImported, err
}

func (s *StakepooldManager) RPCStatus() []string {
	stakepooldPageInfo := make([]string, len(s.grpcConnections))

	for i, conn := range s.grpcConnections {
		grpcStatus := "Unknown"
		state := conn.GetState()
		switch state {
		case connectivity.Idle:
			grpcStatus = "Idle"
		case connectivity.Shutdown:
			grpcStatus = "Shutdown"
		case connectivity.Ready:
			grpcStatus = "Ready"
		case connectivity.Connecting:
			grpcStatus = "Connecting"
		case connectivity.TransientFailure:
			grpcStatus = "TransientFailure"
		}

		stakepooldPageInfo[i] = grpcStatus
	}

	return stakepooldPageInfo
}
