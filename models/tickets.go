// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package models

import (
	"github.com/go-gorp/gorp"
	_ "github.com/go-sql-driver/mysql"
)

type Fees struct {
	TicketHash          string
	CommitmentSignature string
	FeeAddress          string
	Address             string
	SDiff               int64
	BlockHeight         int64
	VoteBits            uint16
	VotingKey           string
}

// GetFeesByFeeAddress is a helper function that returns the associated fee entry
// with then provided feeAddress.
func GetFeesByFeeAddress(dbMap *gorp.DbMap, feeAddress string) (*Fees, error) {
	var fees Fees

	err := dbMap.SelectOne(&fees, "SELECT * FROM Fees WHERE FeeAddress = ?", feeAddress)
	if err != nil {
		return nil, err
	}

	return &fees, nil
}

// GetFeeAddressByTicketHash is a helper function that returns cached getfeeaddress results
func GetFeeAddressByTicketHash(dbMap *gorp.DbMap, ticketHash string) (*Fees, error) {
	var fees Fees

	err := dbMap.SelectOne(&fees, "SELECT * FROM Fees WHERE TicketHash = ?", ticketHash)
	if err != nil {
		log.Warnf("GetFeeAddressByTicketHash: %v", err)
		return nil, err
	}

	return &fees, nil
}

// GetInactiveFeeAddresses is a helper function that returns the fee addresses without an
// associated votingkey
func GetInactiveFeeAddresses(dbMap *gorp.DbMap) ([]string, error) {
	var addrs []string

	_, err := dbMap.Select(&addrs, "SELECT FeeAddress FROM Fees WHERE LENGTH(VotingKey) = 0")
	if err != nil {
		log.Warnf("GetInactiveFeeAddresses: %v", err)
	}

	return addrs, err
}

// InsertFeeAddress inserts a fee address into the DB.
func InsertFeeAddress(dbMap *gorp.DbMap, fee *Fees) error {
	return dbMap.Insert(fee)
}

// InsertFeeAddressVotingKey updates the fees table with a voting key for the given address.
func InsertFeeAddressVotingKey(dbMap *gorp.DbMap, address, votingKey string, voteBits uint16) error {
	_, err := dbMap.Exec("UPDATE Fees SET VotingKey = ?, VoteBits = ? WHERE Address = ?", votingKey, voteBits, address)
	if err != nil {
		log.Warnf("InsertFeeAddressPrivKey: %v", err)
	}
	return err
}
