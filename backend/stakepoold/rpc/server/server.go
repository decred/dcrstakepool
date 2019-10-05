// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package server implements the RPC API and is used by the main package to
// start gRPC services.
//
// Full documentation of the API implemented by this package is maintained in a
// language-agnostic document:
//
// TODO Document gRPC API like dcrwallet once the API is stable
package server

import (
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/decred/dcrd/chaincfg/chainhash"
	pb "github.com/decred/dcrstakepool/backend/stakepoold/rpc/stakepoolrpc"
	"github.com/decred/dcrstakepool/backend/stakepoold/stakepool"
	"github.com/decred/dcrstakepool/backend/stakepoold/userdata"
)

// Public API version constants
const (
	// The most probable reason for a command timing out would be because a
	// deadlock has occurred in the main process.  We want to reply with an
	// error message in this case before dcrstakepool applies a client timeout.
	// The commands are basic map operations and copies and typically complete
	// within one millisecond.  It is possible for an abnormally long garbage
	// collection cycle to also trigger a timeout but the current allocation
	// pattern of stakepoold is not known to cause such conditions at this time.
	GRPCCommandTimeout = time.Millisecond * 100
	semverString       = "9.0.0"
	semverMajor        = 9
	semverMinor        = 0
	semverPatch        = 0
)

// versionServer provides RPC clients with the ability to query the RPC server
// version.
type versionServer struct {
}

// StartVersionService creates an implementation of the VersionService and
// registers it with the gRPC server.
func StartVersionService(server *grpc.Server) {
	pb.RegisterVersionServiceServer(server, &versionServer{})
}

func (v *versionServer) Version(ctx context.Context, req *pb.VersionRequest) (*pb.VersionResponse, error) {
	return &pb.VersionResponse{
		VersionString: semverString,
		Major:         semverMajor,
		Minor:         semverMinor,
		Patch:         semverPatch,
	}, nil
}

// StakepooldServer provides RPC clients with the ability to trigger updates
// to the user voting config
type stakepooldServer struct {
	stakepoold *stakepool.Stakepoold
}

// StartStakepooldService creates an implementation of the StakepooldService
// and registers it.
func StartStakepooldService(stakepoold *stakepool.Stakepoold, server *grpc.Server) {
	pb.RegisterStakepooldServiceServer(server, &stakepooldServer{
		stakepoold: stakepoold,
	})
}

func processTickets(ticketsMSA map[chainhash.Hash]string) []*pb.Ticket {
	tickets := make([]*pb.Ticket, 0)
	for tickethash, msa := range ticketsMSA {
		tickets = append(tickets, &pb.Ticket{
			Address: msa,
			Hash:    tickethash.CloneBytes(),
		})
	}
	return tickets
}

func (s *stakepooldServer) GetAddedLowFeeTickets(c context.Context, req *pb.GetAddedLowFeeTicketsRequest) (*pb.GetAddedLowFeeTicketsResponse, error) {

	s.stakepoold.RLock()
	ticketsMSA := s.stakepoold.AddedLowFeeTicketsMSA
	s.stakepoold.RUnlock()

	tickets := processTickets(ticketsMSA)
	return &pb.GetAddedLowFeeTicketsResponse{Tickets: tickets}, nil
}

func (s *stakepooldServer) GetIgnoredLowFeeTickets(c context.Context, req *pb.GetIgnoredLowFeeTicketsRequest) (*pb.GetIgnoredLowFeeTicketsResponse, error) {

	s.stakepoold.RLock()
	ticketsMSA := s.stakepoold.IgnoredLowFeeTicketsMSA
	s.stakepoold.RUnlock()

	tickets := processTickets(ticketsMSA)
	return &pb.GetIgnoredLowFeeTicketsResponse{Tickets: tickets}, nil
}

func (s *stakepooldServer) GetLiveTickets(c context.Context, req *pb.GetLiveTicketsRequest) (*pb.GetLiveTicketsResponse, error) {

	s.stakepoold.RLock()
	ticketsMSA := s.stakepoold.LiveTicketsMSA
	s.stakepoold.RUnlock()

	tickets := processTickets(ticketsMSA)
	return &pb.GetLiveTicketsResponse{Tickets: tickets}, nil
}

func (s *stakepooldServer) SetAddedLowFeeTickets(ctx context.Context, req *pb.SetAddedLowFeeTicketsRequest) (*pb.SetAddedLowFeeTicketsResponse, error) {

	addedLowFeeTickets := make(map[chainhash.Hash]string)

	for _, ticket := range req.Tickets {
		hash, err := chainhash.NewHash(ticket.Hash)
		if err != nil {
			log.Warnf("NewHashFromStr failed for %v", ticket.Hash)
			continue
		}
		addedLowFeeTickets[*hash] = ticket.Address
	}

	s.stakepoold.UpdateTicketData(addedLowFeeTickets)
	return &pb.SetAddedLowFeeTicketsResponse{}, nil
}

func (s *stakepooldServer) SetUserVotingPrefs(ctx context.Context, req *pb.SetUserVotingPrefsRequest) (*pb.SetUserVotingPrefsResponse, error) {

	userVotingPrefs := make(map[string]userdata.UserVotingConfig)
	for _, data := range req.UserVotingConfig {
		userVotingPrefs[data.MultiSigAddress] = userdata.UserVotingConfig{
			Userid:          data.UserId,
			MultiSigAddress: data.MultiSigAddress,
			VoteBits:        uint16(data.VoteBits),
			VoteBitsVersion: uint32(data.VoteBitsVersion),
		}
	}

	s.stakepoold.UpdateUserData(userVotingPrefs)
	return &pb.SetUserVotingPrefsResponse{}, nil
}

func (s *stakepooldServer) ImportNewScript(ctx context.Context, req *pb.ImportNewScriptRequest) (*pb.ImportNewScriptResponse, error) {
	heightImported, err := s.stakepoold.ImportNewScript(req.Script)
	if err != nil {
		return nil, err
	}
	return &pb.ImportNewScriptResponse{
		HeightImported: heightImported,
	}, nil
}

func (s *stakepooldServer) ImportMissingScripts(ctx context.Context, req *pb.ImportMissingScriptsRequest) (*pb.ImportMissingScriptsResponse, error) {
	err := s.stakepoold.ImportMissingScripts(req.Scripts, int(req.RescanHeight))
	if err != nil {
		return nil, err
	}
	return &pb.ImportMissingScriptsResponse{}, nil
}

func (s *stakepooldServer) ListScripts(ctx context.Context, req *pb.ListScriptsRequest) (*pb.ListScriptsResponse, error) {

	scripts, err := s.stakepoold.ListScripts()
	if err != nil {
		return nil, err
	}

	return &pb.ListScriptsResponse{Scripts: scripts}, nil
}

func (s *stakepooldServer) AccountSyncAddressIndex(ctx context.Context, req *pb.AccountSyncAddressIndexRequest) (*pb.AccountSyncAddressIndexResponse, error) {

	err := s.stakepoold.AccountSyncAddressIndex(req.Account, req.Branch, int(req.Index))
	if err != nil {
		return nil, err
	}

	return &pb.AccountSyncAddressIndexResponse{}, nil
}

func (s *stakepooldServer) GetTickets(ctx context.Context, req *pb.GetTicketsRequest) (*pb.GetTicketsResponse, error) {

	tickets, err := s.stakepoold.GetTickets(req.IncludeImmature)
	if err != nil {
		return nil, err
	}

	// Serialise for sending back over RPC.
	// Need to change *chainhash.Hash into []byte.
	ticketBytes := make([][]byte, len(tickets))
	for i := 0; i < len(tickets); i++ {
		ticketBytes[i] = tickets[i][:]
	}

	return &pb.GetTicketsResponse{Tickets: ticketBytes}, nil
}

func (s *stakepooldServer) AddMissingTicket(ctx context.Context, req *pb.AddMissingTicketRequest) (*pb.AddMissingTicketResponse, error) {

	err := s.stakepoold.AddMissingTicket(req.Hash)
	if err != nil {
		return nil, err
	}
	return &pb.AddMissingTicketResponse{}, nil
}

func (s *stakepooldServer) StakePoolUserInfo(ctx context.Context, req *pb.StakePoolUserInfoRequest) (*pb.StakePoolUserInfoResponse, error) {

	response, err := s.stakepoold.StakePoolUserInfo(req.MultiSigAddress)
	if err != nil {
		return nil, err
	}

	tickets := make([]*pb.StakePoolUserTicket, 0, len(response.Tickets))
	for _, t := range response.Tickets {
		tickets = append(tickets, &pb.StakePoolUserTicket{
			Status:        t.Status,
			Ticket:        t.Ticket,
			TicketHeight:  t.TicketHeight,
			SpentBy:       t.SpentBy,
			SpentByHeight: t.SpentByHeight,
		})
	}

	return &pb.StakePoolUserInfoResponse{
		Tickets:        tickets,
		InvalidTickets: response.InvalidTickets,
	}, nil
}

func (s *stakepooldServer) WalletInfo(ctx context.Context, req *pb.WalletInfoRequest) (*pb.WalletInfoResponse, error) {

	response, err := s.stakepoold.WalletInfo()
	if err != nil {
		return nil, err
	}

	return &pb.WalletInfoResponse{
		VoteVersion:     response.VoteVersion,
		DaemonConnected: response.DaemonConnected,
		Unlocked:        response.Unlocked,
		Voting:          response.Voting,
	}, nil
}

func (s *stakepooldServer) ValidateAddress(ctx context.Context, req *pb.ValidateAddressRequest) (*pb.ValidateAddressResponse, error) {

	response, err := s.stakepoold.ValidateAddress(req.Address)
	if err != nil {
		return nil, err
	}

	return &pb.ValidateAddressResponse{
		IsMine:     response.IsMine,
		PubKeyAddr: response.PubKeyAddr,
	}, nil
}

func (s *stakepooldServer) CreateMultisig(ctx context.Context, req *pb.CreateMultisigRequest) (*pb.CreateMultisigResponse, error) {

	response, err := s.stakepoold.CreateMultisig(req.Address)
	if err != nil {
		return nil, err
	}

	return &pb.CreateMultisigResponse{
		RedeemScript: response.RedeemScript,
		Address:      response.Address,
	}, nil
}

func (s *stakepooldServer) GetStakeInfo(ctx context.Context, req *pb.GetStakeInfoRequest) (*pb.GetStakeInfoResponse, error) {

	response, err := s.stakepoold.GetStakeInfo()
	if err != nil {
		return nil, err
	}
	return &pb.GetStakeInfoResponse{
		BlockHeight:      response.BlockHeight,
		Difficulty:       response.Difficulty,
		TotalSubsidy:     response.TotalSubsidy,
		OwnMempoolTix:    response.OwnMempoolTix,
		Immature:         response.Immature,
		Unspent:          response.Unspent,
		Voted:            response.Voted,
		Revoked:          response.Revoked,
		UnspentExpired:   response.UnspentExpired,
		PoolSize:         response.PoolSize,
		AllMempoolTix:    response.AllMempoolTix,
		Live:             response.Live,
		ProportionLive:   response.ProportionLive,
		Missed:           response.Missed,
		ProportionMissed: response.ProportionMissed,
		Expired:          response.Expired,
	}, nil
}

func (s *stakepooldServer) GetColdWalletExtPub(ctx context.Context, req *pb.GetColdWalletExtPubRequest) (*pb.GetColdWalletExtPubResponse, error) {
	return &pb.GetColdWalletExtPubResponse{
		ColdWalletExtPub: s.stakepoold.ColdWalletExtPub,
	}, nil
}
