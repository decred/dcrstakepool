// Copyright (c) 2019 The Decred developers

// Package dcrwallet provides a gRPC client to communicate with dcrwallet.
package dcrwallet

import (
	"context"
	"fmt"
	"sync"

	pb "github.com/decred/dcrwallet/rpc/walletrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	requiredDCRwalletAPI = semver{major: 7, minor: 2, patch: 0}
)

type Client struct {
	conn *grpc.ClientConn
}

// New creates a new client connections from the specified certificate file and
// host. Starts a go routine that waits for context Done to close the connection.
// Returns an error if unable to establish a connection or semver is incompatable.
func New(ctx context.Context, wg *sync.WaitGroup, certFile, host string) (*Client, error) {
	const op = "client.dcrwallet.New"
	creds, err := credentials.NewClientTLSFromFile(certFile, "localhost")
	if err != nil {
		return nil, fmt.Errorf("%v: failed to read dcrwallet cert file at %s: %v\n", op,
			host, err)
	}
	log.Infof("Attempting to connect to dcrwalletGRPC %s using certificate located in %s",
		host, certFile)
	conn, err := grpc.Dial(host, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("%v: %v", op, err)
	}
	c := &Client{}
	c.conn = conn
	if err := c.versionOK(ctx, host); err != nil {
		if err := c.conn.Close(); err != nil {
			log.Debugf("%v: wallet GRPC close error: %v", op, err)
		}
		return nil, fmt.Errorf("%v: version check failed: %v", op, err)
	}
	// Close connection on shutdown signal.
	wg.Add(1)
	go func() {
		<-ctx.Done()
		if err := c.conn.Close(); err != nil {
			log.Debugf("wallet GRPC close error: %v", err)
		}
		wg.Done()
	}()
	log.Infof("Established connection to gRPC server %s", host)
	return c, nil
}

func (c *Client) versionOK(ctx context.Context, host string) error {
	ver, err := c.version(ctx)
	if err != nil {
		return err
	}
	var semverResponse = semver{
		major: ver.Major,
		minor: ver.Minor,
		patch: ver.Patch,
	}
	if !semverCompatible(requiredDCRwalletAPI, semverResponse) {
		return fmt.Errorf("dcrwallet gRPC server %s does not have "+
			"a compatible API version. Advertises %v but require %v",
			host, ver, requiredDCRwalletAPI)
	}
	return nil
}

func (c *Client) version(ctx context.Context) (*pb.VersionResponse, error) {
	client := pb.NewVersionServiceClient(c.conn)
	return client.Version(ctx, &pb.VersionRequest{})
}
