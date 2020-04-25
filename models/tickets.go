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

// InsertFeeAddress inserts a fee address into the DB.
func InsertFeeAddress(dbMap *gorp.DbMap, fee *Fees) error {
	return dbMap.Insert(fee)
}
