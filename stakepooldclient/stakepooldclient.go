package stakepooldclient

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/decred/dcrd/chaincfg/chainhash"
	pb "github.com/decred/dcrstakepool/backend/stakepoold/rpc/stakepoolrpc"
	"github.com/decred/dcrstakepool/models"
	"golang.org/x/net/context"
)

var requiredStakepooldAPI = semver{major: 4, minor: 0, patch: 0}

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

	log.Infof("Established connection to gRPC server %s",
		stakepooldHosts[serverID])

	return conn, nil
}

func StakepooldGetAddedLowFeeTickets(conn *grpc.ClientConn) (map[chainhash.Hash]string, error) {
	addedLowFeeTickets := make(map[chainhash.Hash]string)

	client := pb.NewStakepooldServiceClient(conn)
	resp, err := client.GetAddedLowFeeTickets(context.Background(), &pb.GetAddedLowFeeTicketsRequest{})
	// return early if the list is empty
	if resp == nil || err != nil {
		return addedLowFeeTickets, err
	}

	for _, ticketData := range resp.Tickets {
		hash, err := chainhash.NewHash(ticketData.TicketHash)
		if err != nil {
			log.Warnf("NewHash failed for %v: %v", ticketData.TicketHash, err)
			continue
		}
		addedLowFeeTickets[*hash] = ticketData.TicketAddress
	}

	return addedLowFeeTickets, err
}

func StakepooldGetIgnoredLowFeeTickets(conn *grpc.ClientConn) (map[chainhash.Hash]string, error) {
	ignoredLowFeeTickets := make(map[chainhash.Hash]string)

	client := pb.NewStakepooldServiceClient(conn)
	resp, err := client.GetIgnoredLowFeeTickets(context.Background(), &pb.GetIgnoredLowFeeTicketsRequest{})
	// return early if the list is empty
	if resp == nil || err != nil {
		return ignoredLowFeeTickets, err
	}

	for _, ticketData := range resp.Tickets {
		hash, err := chainhash.NewHash(ticketData.TicketHash)
		if err != nil {
			log.Warnf("NewHash failed for %v: %v", ticketData.TicketHash, err)
			continue
		}
		ignoredLowFeeTickets[*hash] = ticketData.TicketAddress
	}

	return ignoredLowFeeTickets, err
}

func StakepooldGetLiveTickets(conn *grpc.ClientConn) (map[chainhash.Hash]string, error) {
	liveTickets := make(map[chainhash.Hash]string)

	client := pb.NewStakepooldServiceClient(conn)
	resp, err := client.GetLiveTickets(context.Background(), &pb.GetLiveTicketsRequest{})
	// return early if the list is empty
	if resp == nil || err != nil {
		return liveTickets, err
	}

	for _, ticketData := range resp.Tickets {
		hash, err := chainhash.NewHash(ticketData.TicketHash)
		if err != nil {
			log.Warnf("NewHash failed for %v: %v", ticketData.TicketHash, err)
			continue
		}
		liveTickets[*hash] = ticketData.TicketAddress
	}

	return liveTickets, err
}

func StakepooldSetAddedLowFeeTickets(conn *grpc.ClientConn, dbTickets []models.LowFeeTicket) (processed bool, err error) {
	var tickets []*pb.TicketEntry
	for _, ticket := range dbTickets {
		hash, err := chainhash.NewHashFromStr(ticket.TicketHash)
		if err != nil {
			log.Warnf("NewHashFromStr failed for %v: %v", ticket.TicketHash, err)
			continue
		}
		tickets = append(tickets, &pb.TicketEntry{
			TicketAddress: ticket.TicketAddress,
			TicketHash:    hash.CloneBytes(),
		})
	}

	client := pb.NewStakepooldServiceClient(conn)
	setAddedTicketsReq := &pb.SetAddedLowFeeTicketsRequest{
		Tickets: tickets,
	}
	_, err = client.SetAddedLowFeeTickets(context.Background(),
		setAddedTicketsReq)
	if err != nil {
		return false, err
	}
	return true, err
}

func StakepooldSetUserVotingPrefs(conn *grpc.ClientConn, dbUsers map[int64]*models.User) (processed bool, err error) {
	var users []*pb.UserVotingConfigEntry
	for userid, data := range dbUsers {
		users = append(users, &pb.UserVotingConfigEntry{
			UserId:          userid,
			MultiSigAddress: data.MultiSigAddress,
			VoteBits:        data.VoteBits,
			VoteBitsVersion: data.VoteBitsVersion,
		})
	}

	client := pb.NewStakepooldServiceClient(conn)
	setVotingConfigReq := &pb.SetUserVotingPrefsRequest{
		UserVotingConfig: users,
	}
	_, err = client.SetUserVotingPrefs(context.Background(),
		setVotingConfigReq)
	if err != nil {
		return false, err
	}
	return true, err
}
