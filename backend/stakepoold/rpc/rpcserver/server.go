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
//   https://github.com/decred/dcrwallet/blob/master/rpc/documentation/api.md
//
// Any API changes must be performed according to the steps listed here:
//
//   https://github.com/decred/dcrwallet/blob/master/rpc/documentation/serverchanges.md
package rpcserver

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	pb "github.com/decred/dcrstakepool/backend/stakepoold/rpc/stakepoolrpc"
)

// Public API version constants
const (
	semverString = "3.0.0"
	semverMajor  = 3
	semverMinor  = 0
	semverPatch  = 0
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

func (*versionServer) Version(ctx context.Context, req *pb.VersionRequest) (*pb.VersionResponse, error) {
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
	c chan struct{}
}

// StartStakepooldService creates an implementation of the StakepooldService
// and registers it.
func StartStakepooldService(c chan struct{}, server *grpc.Server) {
	pb.RegisterStakepooldServiceServer(server, &stakepooldServer{
		c: c,
	})
}

func (s *stakepooldServer) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingResponse, error) {
	return &pb.PingResponse{}, nil
}

func (s *stakepooldServer) UpdateVotingPrefs(ctx context.Context, req *pb.UpdateVotingPrefsRequest) (*pb.UpdateVotingPrefsResponse, error) {
	defer func() {
		// Don't block on messaging.  We want to make sure we can
		// handle the next call ASAP.
		select {
		case s.c <- struct{}{}:
		default:
			// We log this in order to detect if we potentially
			// have a deadlock.
			log.Infof("Reload user config message not sent")
		}
	}()
	return &pb.UpdateVotingPrefsResponse{}, nil
}
