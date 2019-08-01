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

	MaxTicketChallengeAge = 60 * 30 // 30 minutes
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

	// check if timestamp is not yet expired and has not been used previously
	if err := v3Api.validateTimestamp(timestamp, v3Api.ticketChallengeMaxAge); err != nil {
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

func (v3Api *V3API) validateTimestamp(timestampMessage string, ticketChallengeMaxAge int64) error {
	authTimestamp, err := strconv.Atoi(timestampMessage)
	if err != nil {
		return fmt.Errorf("invalid timestamp value %v: %v", timestampMessage, err)
	}

	// Ensure that timestamp had not been used in a previous authentication attempt.
	if v3Api.processedTicketChallenges.containsChallenge(timestampMessage) {
		return fmt.Errorf("disallowed reuse of timestamp value %v", timestampMessage)
	}

	// Ensure that the auth timestamp is not in the future and is not more than 30 seconds into the past.
	timestampDelta := time.Now().Unix() - int64(authTimestamp)
	if timestampDelta < 0 || timestampDelta > ticketChallengeMaxAge {
		return fmt.Errorf("expired timestamp value %v", timestampMessage)
	}

	// Save this timestamp value as used to prevent subsequent reuse.
	challengeExpiresIn := ticketChallengeMaxAge - timestampDelta
	v3Api.processedTicketChallenges.addChallenge(timestampMessage, challengeExpiresIn)

	return nil
}
