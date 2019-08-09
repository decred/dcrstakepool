package v3api

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrwallet/wallet"
	"fmt"
)

const (
	customAuthScheme          = "TicketAuth"
	customAuthTimestampParam  = "SignedTimestamp"
	customAuthSignatureParam  = "Signature"
	customAuthTicketHashParam = "TicketHash"

	MaxTicketChallengeAge = 60 * 30 // 30 minutes
)

func (v3Api *V3API) validateTicketOwnership(authHeader string) (multiSigAddress, authValidationFailureReason string) {
	if !strings.HasPrefix(authHeader, customAuthScheme) {
		authValidationFailureReason = fmt.Sprintf("invalid API v3 auth header value %s", authHeader)
		return
	}

	timestamp, timestampSignature, ticketHash := extractAuthParams(strings.TrimPrefix(authHeader, customAuthScheme))
	if timestamp == "" || timestampSignature == "" || ticketHash == "" {
		authValidationFailureReason = fmt.Sprintf("invalid API v3 auth header value %s", authHeader)
		return
	}

	// Ensure that this signature had not been used in a previous authentication attempt.
	if v3Api.processedTicketChallenges.containsChallenge(timestampSignature) {
		authValidationFailureReason = fmt.Sprintf("disallowed reuse of ticket auth signature %v", timestampSignature)
		return
	}

	authTimestamp, err := strconv.Atoi(timestamp)
	if err != nil {
		authValidationFailureReason = fmt.Sprintf("invalid ticket auth timestamp value %v", timestamp)
		return
	}

	// Ensure that the auth timestamp is not in the future and is not more than 30 seconds into the past.
	timestampDelta := time.Now().Unix() - int64(authTimestamp)
	if timestampDelta < 0 || timestampDelta > v3Api.ticketChallengeMaxAge {
		authValidationFailureReason = fmt.Sprintf("expired ticket auth timestamp value %v", timestamp)
		return
	}

	// confirm that the timestamp signature is a valid base64 string
	decodedSignature, err := base64.StdEncoding.DecodeString(timestampSignature)
	if err != nil {
		authValidationFailureReason = fmt.Sprintf("invalid ticket auth signature %s", timestampSignature)
		return
	}

	// Mark this timestamp signature as used to prevent subsequent reuse.
	challengeExpiresIn := v3Api.ticketChallengeMaxAge - timestampDelta
	v3Api.processedTicketChallenges.addChallenge(timestampSignature, challengeExpiresIn)

	// todo check if ticket belongs to this vsp

	// get user wallet address using ticket hash
	// todo: may be better to maintain a memory map of tickets-userWalletAddresses
	ticketInfo, err := v3Api.stakepooldConnMan.GetTicketInfo(ticketHash)
	if err != nil {
		authValidationFailureReason = fmt.Sprintf("ticket auth, get ticket info failed: %v", err)
		return
	}

	// check if timestamp signature checks out against address
	addr, err := dcrutil.DecodeAddress(ticketInfo.OwnerFeeAddress)
	if err != nil {
		authValidationFailureReason = fmt.Sprintf("ticket auth, unexpected decode address error: %v", err)
		return
	}

	valid, err := wallet.VerifyMessage(timestamp, addr, decodedSignature)
	if err != nil {
		authValidationFailureReason = fmt.Sprintf("error validating timestamp signature for ticket auth %v", err)
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
