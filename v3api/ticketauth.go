package v3api

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrwallet/wallet"
)

const (
	customAuthScheme          = "TicketAuth"
	customAuthTimestampParam  = "SignedTimestamp"
	customAuthSignatureParam  = "Signature"
	customAuthTicketHashParam = "TicketHash"

	authTimestampValiditySeconds = 30 * 10000000000
)

func (v3Api *V3API) validateTicketOwnership(authHeader string) (multiSigAddress string) {
	if !strings.HasPrefix(authHeader, customAuthScheme) {
		log.Warnf("invalid API v3 auth header value %s", authHeader)
		return
	}

	timestamp, timestampSignature, ticketHash := extractAuthParams(strings.TrimPrefix(authHeader, customAuthScheme))
	if timestamp == "" || timestampSignature == "" || ticketHash == "" {
		log.Warnf("invalid API v3 auth header value %s", authHeader)
		return
	}

	// confirm that the timestamp signature is a valid base64 string
	decodedSignature, err := base64.StdEncoding.DecodeString(timestampSignature)
	if err != nil {
		log.Warnf("invalid API v3 signature %s", timestampSignature)
		return
	}

	// todo check if ticket belongs to this vsp

	// check if timestamp is not yet expired
	if err := validateTimestamp(timestamp); err != nil {
		log.Warnf("ticket auth timestamp failed validation: %v", err)
		return
	}

	// get user wallet address using ticket hash
	// todo: may be better to maintain a memory map of tickets-userWalletAddresses
	ticketInfo, err := v3Api.stakepooldConnMan.GetTicketInfo(ticketHash)
	if err != nil {
		log.Warnf("ticket auth, get ticket info failed: %v", err)
		return
	}

	// check if timestamp signature checks out against address
	addr, err := dcrutil.DecodeAddress(ticketInfo.OwnerFeeAddress)
	if err != nil {
		log.Errorf("ticket auth, unexpected decode address error: %v", err)
		return
	}

	valid, err := wallet.VerifyMessage(timestamp, addr, decodedSignature)
	if err != nil {
		log.Errorf("error validating timestamp signature for ticket auth %v", err)
		return
	}

	if valid {
		multiSigAddress = ticketInfo.MultiSigAddress
	}
	return
}

func extractAuthParams(authHeader string) (timestampMessage, timestampSignature, ticketHash string) {
	authParams := strings.Split(authHeader, ",")
	for _, param := range authParams {
		paramKeyValue := strings.TrimSpace(param)
		if value := getAuthValueFromParam(paramKeyValue, customAuthTimestampParam); value != "" {
			timestampMessage = strings.TrimSpace(value)
		} else if value := getAuthValueFromParam(paramKeyValue, customAuthSignatureParam); value != "" {
			timestampSignature = strings.TrimSpace(value)
		} else if value := getAuthValueFromParam(paramKeyValue, customAuthTicketHashParam); value != "" {
			ticketHash = strings.TrimSpace(value)
		}
	}
	return
}

func getAuthValueFromParam(paramKeyValue, key string) string {
	keyPrefix := key + "="
	if strings.HasPrefix(paramKeyValue, keyPrefix) {
		return strings.TrimPrefix(paramKeyValue, keyPrefix)
	}
	return ""
}

func validateTimestamp(timestampMessage string) error {
	authTimestamp, err := strconv.Atoi(timestampMessage)
	if err != nil {
		return fmt.Errorf("invalid v3 auth request timestamp %v: %v", timestampMessage, err)
	}

	// todo ensure that timestamp had not been used in a previous authentication attempt

	// Ensure that the auth timestamp is not in the future and is not more than 30 seconds into the past.
	timestampDelta := time.Now().Unix() - int64(authTimestamp)
	if timestampDelta < 0 || timestampDelta > authTimestampValiditySeconds {
		return fmt.Errorf("expired v3 auth request timestamp %v compared to %v", timestampMessage, time.Now().Unix())
	}

	return nil
}
