// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package rpcserver implements the RPC API and is used by the main package to
// start gRPC services.
//
// Full documentation of the API implemented by this package is maintained in a
// language-agnostic document:
//
// TODO Document gRPC API like dcrwallet once the API is stable
package rpcserver

import (
	"fmt"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/decred/dcrd/chaincfg/chainhash"
	pb "github.com/decred/dcrstakepool/backend/stakepoold/rpc/stakepoolrpc"
	"github.com/decred/dcrstakepool/backend/stakepoold/userdata"
)

// Public API version constants
const (
	// This should only get hit if stakepoold has deadlocked.  Most requests
	// should be fulfilled within ~1ms.  This just makes sure dcrstakepool gets
	// a response of some sort even if stakepoold has indeed deadlocked.
	GRPCCommandTimeout = time.Millisecond * 100
	semverString       = "4.0.0"
	semverMajor        = 4
	semverMinor        = 0
	semverPatch        = 0
)

// CommandName maps function names to an integer.
type CommandName int

func (s CommandName) String() string {
	switch s {
	case GetAddedLowFeeTickets:
		return "GetAddedLowFeeTickets"
	case GetIgnoredLowFeeTickets:
		return "GetIgnoredLowFeeTickets"
	case GetLiveTickets:
		return "GetLiveTickets"
	case SetAddedLowFeeTickets:
		return "SetAddedLowFeeTickets"
	case SetUserVotingPrefs:
		return "SetUserVotingPrefs"
	default:
		log.Errorf("unknown command: %d", s)
		return "UnknownCmd"
	}
}

const (
	GetAddedLowFeeTickets CommandName = iota
	GetIgnoredLowFeeTickets
	GetLiveTickets
	SetAddedLowFeeTickets
	SetUserVotingPrefs
)

type GRPCCommandQueue struct {
	Command                CommandName
	RequestTicketData      map[chainhash.Hash]string
	RequestUserData        map[string]userdata.UserVotingConfig
	ResponseEmptyChan      chan struct{}
	ResponseTicketsMSAChan chan map[chainhash.Hash]string
}

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
	grpcCommandQueueChan chan *GRPCCommandQueue
}

// StartStakepooldService creates an implementation of the StakepooldService
// and registers it.
func StartStakepooldService(grpcCommandQueueChan chan *GRPCCommandQueue, server *grpc.Server) {
	pb.RegisterStakepooldServiceServer(server, &stakepooldServer{
		grpcCommandQueueChan: grpcCommandQueueChan,
	})
}

func (s *stakepooldServer) processSetCommand(ctx context.Context, cmd *GRPCCommandQueue) error {
	// send gRPC command to the handler in main
	select {
	case s.grpcCommandQueueChan <- cmd:
		select {
		case <-cmd.ResponseEmptyChan:
			// either it worked or there's a deadlock and timeout will happen
			return nil
		case <-ctx.Done():
			// hit the timeout
			return ctx.Err()
		}
	default:
		// potentially have a deadlock or a closed/invalid channel
		err := fmt.Errorf("%v: unable to signal stakepoold", cmd.Command.String())
		log.Debug(err)
		return err
	}
}

func (s *stakepooldServer) processTicketCommand(ctx context.Context, cmd *GRPCCommandQueue) ([]*pb.TicketEntry, error) {
	tickets := make([]*pb.TicketEntry, 0)

	// send gRPC command to the handler in main
	select {
	case s.grpcCommandQueueChan <- cmd:
		select {
		case ticketsResponse := <-cmd.ResponseTicketsMSAChan:
			// format and return the gRPC response
			for tickethash, msa := range ticketsResponse {
				tickets = append(tickets, &pb.TicketEntry{
					TicketAddress: msa,
					TicketHash:    tickethash.CloneBytes(),
				})
			}
			return tickets, nil
		case <-ctx.Done():
			// hit the timeout
			return nil, ctx.Err()
		}
	default:
		// potentially have a deadlock or a closed/invalid channel
		err := fmt.Errorf("%v: unable to signal stakepoold", cmd.Command.String())
		log.Debug(err)
		return nil, err
	}
}

func (s *stakepooldServer) GetAddedLowFeeTickets(ctx context.Context, req *pb.GetAddedLowFeeTicketsRequest) (*pb.GetAddedLowFeeTicketsResponse, error) {
	tickets, err := s.processTicketCommand(ctx, &GRPCCommandQueue{
		Command:                GetAddedLowFeeTickets,
		ResponseTicketsMSAChan: make(chan map[chainhash.Hash]string),
	})
	return &pb.GetAddedLowFeeTicketsResponse{Tickets: tickets}, err
}

func (s *stakepooldServer) GetIgnoredLowFeeTickets(ctx context.Context, req *pb.GetIgnoredLowFeeTicketsRequest) (*pb.GetIgnoredLowFeeTicketsResponse, error) {
	tickets, err := s.processTicketCommand(ctx, &GRPCCommandQueue{
		Command:                GetIgnoredLowFeeTickets,
		ResponseTicketsMSAChan: make(chan map[chainhash.Hash]string),
	})
	return &pb.GetIgnoredLowFeeTicketsResponse{Tickets: tickets}, err
}

func (s *stakepooldServer) GetLiveTickets(ctx context.Context, req *pb.GetLiveTicketsRequest) (*pb.GetLiveTicketsResponse, error) {
	tickets, err := s.processTicketCommand(ctx, &GRPCCommandQueue{
		Command:                GetLiveTickets,
		ResponseTicketsMSAChan: make(chan map[chainhash.Hash]string),
	})
	return &pb.GetLiveTicketsResponse{Tickets: tickets}, err
}

func (s *stakepooldServer) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingResponse, error) {
	return &pb.PingResponse{}, nil
}

func (s *stakepooldServer) SetAddedLowFeeTickets(ctx context.Context, req *pb.SetAddedLowFeeTicketsRequest) (*pb.SetAddedLowFeeTicketsResponse, error) {
	addedLowFeeTickets := make(map[chainhash.Hash]string)

	for _, data := range req.Tickets {
		hash, err := chainhash.NewHash(data.TicketHash)
		if err != nil {
			log.Warnf("NewHashFromStr failed for %v", data.TicketHash)
			continue
		}
		addedLowFeeTickets[*hash] = data.TicketAddress
	}

	err := s.processSetCommand(ctx, &GRPCCommandQueue{
		Command:           SetAddedLowFeeTickets,
		RequestTicketData: addedLowFeeTickets,
		ResponseEmptyChan: make(chan struct{}),
	})
	if err == nil {
		return &pb.SetAddedLowFeeTicketsResponse{Processed: true}, nil
	}
	return &pb.SetAddedLowFeeTicketsResponse{Processed: false}, err
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

	err := s.processSetCommand(ctx, &GRPCCommandQueue{
		Command:           SetUserVotingPrefs,
		RequestUserData:   userVotingPrefs,
		ResponseEmptyChan: make(chan struct{}),
	})
	if err == nil {
		return &pb.SetUserVotingPrefsResponse{Processed: true}, nil
	}
	return &pb.SetUserVotingPrefsResponse{Processed: false}, err
}
