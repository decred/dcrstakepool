package system

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrwallet/wallet"
	"github.com/dgrijalva/jwt-go"
)

func (application *Application) validateToken(authHeader string) (int64, string) {
	apitoken := strings.TrimPrefix(authHeader, "Bearer ")

	JWTtoken, err := jwt.Parse(apitoken, func(token *jwt.Token) (interface{}, error) {
		// validate signing algorithm
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(application.APISecret), nil
	})

	if err != nil {
		return -1, fmt.Sprintf("invalid token %v: %v", apitoken, err)
	} else if claims, ok := JWTtoken.Claims.(jwt.MapClaims); ok && JWTtoken.Valid {
		return int64(claims["loggedInAs"].(float64)), ""
	} else {
		return -1, fmt.Sprintf("invalid token %v", apitoken)
	}
}

func (application *Application) validateTicketOwnership(authHeader string) (multiSigAddress, authValidationFailureReason string) {
	timestamp, timestampSignature, ticketHash := extractTicketAuthParams(strings.TrimPrefix(authHeader, "TicketAuth "))
	if timestamp == "" || timestampSignature == "" || ticketHash == "" {
		authValidationFailureReason = fmt.Sprintf("invalid ticket auth header value %s", authHeader)
		return
	}

	// Ensure that this signature had not been used in a previous authentication attempt.
	if application.ProcessedTicketChallenges.ContainsChallenge(timestampSignature) {
		authValidationFailureReason = fmt.Sprintf("disallowed reuse of ticket auth signature %v", timestampSignature)
		return
	}

	authTimestamp, err := strconv.Atoi(timestamp)
	if err != nil {
		authValidationFailureReason = fmt.Sprintf("invalid ticket auth timestamp value %v", timestamp)
		return
	}

	// Ensure that the auth timestamp
	// - is not more than 5 minutes into the future
	// - is not more than 30 seconds into the past
	timestampDelta := time.Now().Unix() - int64(authTimestamp)
	if timestampDelta < -300 {
		// more than 5 minutes into the future
		authValidationFailureReason = "invalid (future) timestamp"
		return
	} else if timestampDelta > application.TicketChallengeMaxAge {
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
	challengeExpiresIn := application.TicketChallengeMaxAge - timestampDelta
	application.ProcessedTicketChallenges.AddChallenge(timestampSignature, challengeExpiresIn)

	// todo check if ticket belongs to this vsp

	// get user wallet address using ticket hash
	// todo: may be better to maintain a memory map of tickets-userWalletAddresses
	ticketInfo, err := application.StakepooldConnMan.GetTicketInfo(ticketHash)
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

func extractTicketAuthParams(authHeader string) (timestampMessage, timestampSignature, ticketHash string) {
	authParams := strings.Split(authHeader, ",")
	for _, param := range authParams {
		paramKeyValue := strings.TrimSpace(param)
		if value := getAuthValueFromParam(paramKeyValue, "SignedTimestamp"); value != "" {
			timestampMessage = strings.TrimSpace(value)
		} else if value := getAuthValueFromParam(paramKeyValue, "Signature"); value != "" {
			timestampSignature = strings.TrimSpace(value)
		} else if value := getAuthValueFromParam(paramKeyValue, "TicketHash"); value != "" {
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
