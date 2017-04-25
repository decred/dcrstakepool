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
	"github.com/decred/dcrstakepool/backend/stakepoold/voteoptions"
)

// Public API version constants
const (
	semverString = "2.0.0"
	semverMajor  = 2
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

// StakepooldServer provides RPC clients with the ability to query the RPC
// server for the current VoteVersion and VoteInfo which contains options
type stakepooldServer struct {
	c chan struct{}
}

// voteOptionsServer provides RPC clients with the ability to query the RPC
// server for the current VoteVersion and VoteInfo which contains options
type voteOptionsConfigServer struct {
}

// StartStakepooldService creates an implementation of the StakepooldService
// and registers it.
func StartStakepooldService(c chan struct{}, vo *voteoptions.VoteOptions, server *grpc.Server) {
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

func (s *stakepooldServer) VoteOptionsConfig(ctx context.Context, req *pb.VoteOptionsConfigRequest) (*pb.VoteOptionsConfigResponse, error) {
	// TODO remove this hack once decrediton/paymetheus testing is done
	// TODO switch to using chainparams?
	voteInfo := []string{
		`{
  "currentheight": 121740,
  "startheight": 116992,
  "endheight": 125055,
  "hash": "00000000000000d6eb790b4983a0e36a0cb47e0ea97c898af6a4d23491737262",
  "voteversion": 4,
  "quorum": 4032,
  "totalvotes": 0,
  "agendas": [
    {
      "id": "lnsupport",
      "description": "Should decred add Lightning Support (LN)?",
      "mask": 6,
      "starttime": 1496275200,
      "expiretime": 1504224000,
      "status": "defined",
      "quorumprogress": 0,
      "choices": [
        {
          "id": "abstain",
          "description": "abstain voting for change",
          "bits": 0,
          "isignore": true,
          "isno": false,
          "count": 0,
          "progress": 0
        },
        {
          "id": "no",
          "description": "reject adding LN support",
          "bits": 2,
          "isignore": false,
          "isno": true,
          "count": 0,
          "progress": 0
        },
        {
          "id": "yes",
          "description": "accept adding LN support",
          "bits": 4,
          "isignore": false,
          "isno": false,
          "count": 0,
          "progress": 0
        }
      ]
    },
    {
      "id": "sdiffalgorithm",
      "description": "Should decred adopt the new SDiff algorithm?",
      "mask": 24,
      "starttime": 1496275200,
      "expiretime": 1504224000,
      "status": "defined",
      "quorumprogress": 0,
      "choices": [
        {
          "id": "abstain",
          "description": "abstain voting for change",
          "bits": 0,
          "isignore": true,
          "isno": false,
          "count": 0,
          "progress": 0
        },
        {
          "id": "no",
          "description": "reject new SDiff algorithm",
          "bits": 8,
          "isignore": false,
          "isno": true,
          "count": 0,
          "progress": 0
        },
        {
          "id": "yes",
          "description": "accept new SDiff algorithm",
          "bits": 16,
          "isignore": false,
          "isno": false,
          "count": 0,
          "progress": 0
        }
      ]
    }
  ]
}`,
		`{
  "currentheight": 15222,
  "startheight": 10848,
  "endheight": 15887,
  "hash": "000000000331d42fd0ba79466e9381582013c7dc99f136057e1854018d49ace7",
  "voteversion": 4,
  "quorum": 2520,
  "totalvotes": 21109
}`}

	return &pb.VoteOptionsConfigResponse{
		VoteInfo:    voteInfo[0],
		VoteVersion: 4,
	}, nil
}
