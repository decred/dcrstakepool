package main

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/decred/dcrstakepool/backend/stakepoold/rpc/stakepoolrpc"
	"golang.org/x/net/context"
)

var requiredStakepooldAPI = semver{major: 1, minor: 0, patch: 0}

func stakepooldGetVoteOptions(conn *grpc.ClientConn) (uint32, string, error) {
	c := pb.NewVoteOptionsConfigServiceClient(conn)
	voteOptionsRequest := &pb.VoteOptionsConfigRequest{}
	voteOptionsResponse, err := c.VoteOptionsConfig(context.Background(), voteOptionsRequest)
	if err != nil {
		return 0, "", err
	}

	return voteOptionsResponse.VoteVersion, voteOptionsResponse.VoteInfo, err
}

func connectStakepooldGRPC(cfg *config, serverID int) (*grpc.ClientConn, error) {
	log.Infof("Attempting to connect to stakepoold gRPC %s using "+
		"certificate located in %s", cfg.StakepooldHosts[serverID],
		cfg.StakepooldCerts[serverID])
	creds, err := credentials.NewClientTLSFromFile(cfg.StakepooldCerts[serverID], "localhost")
	if err != nil {
		return nil, err
	}
	conn, err := grpc.Dial(cfg.StakepooldHosts[serverID], grpc.WithTransportCredentials(creds))
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

	return conn, nil
}
