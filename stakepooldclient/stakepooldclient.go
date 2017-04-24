package stakepooldclient

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/decred/dcrstakepool/backend/stakepoold/rpc/stakepoolrpc"
	"golang.org/x/net/context"
)

var requiredStakepooldAPI = semver{major: 3, minor: 0, patch: 0}

func ConnectStakepooldGRPC(stakepooldHosts []string, stakepooldCerts []string, serverID int) (*grpc.ClientConn, error) {
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

	return conn, nil
}

func StakepooldUpdateVotingPrefs(conn *grpc.ClientConn, userid int64) error {
	c := pb.NewStakepooldServiceClient(conn)
	updateVotingPrefsRequest := &pb.UpdateVotingPrefsRequest{
		Userid: userid,
	}
	_, err := c.UpdateVotingPrefs(context.Background(), updateVotingPrefsRequest)
	return err
}
