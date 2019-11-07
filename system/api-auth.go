package system

import (
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrwallet/wallet/v3"
	"github.com/dgrijalva/jwt-go"
)

// maxTicketChallengeAge is the maximum number of seconds into the past
// or future required for a ticket auth timestamp value to be considered valid.
const maxTicketChallengeAge = 60

func (application *Application) validateToken(authHeader string) (int64, error) {
	apitoken := strings.TrimPrefix(authHeader, "Bearer ")

	JWTtoken, err := jwt.Parse(apitoken, func(token *jwt.Token) (interface{}, error) {
		// validate signing algorithm
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(application.APISecret), nil
	})

	if err != nil {
		return -1, fmt.Errorf("invalid token %v: %v", apitoken, err)
	} else if claims, ok := JWTtoken.Claims.(jwt.MapClaims); ok && JWTtoken.Valid {
		return int64(claims["loggedInAs"].(float64)), nil
	} else {
		return -1, fmt.Errorf("invalid token %v", apitoken)
	}
}

func (application *Application) validateTicketOwnership(authHeader string) (string, error) {
	timestamp, timestampSignature, ticketHash := extractTicketAuthParams(strings.TrimPrefix(authHeader, "TicketAuth "))
	if timestamp == "" || timestampSignature == "" || ticketHash == "" {
		return "", fmt.Errorf("invalid ticket auth header value %s", authHeader)
	}

	// Ensure that this signature had not been used in a previous authentication attempt.
	if application.ProcessedTicketChallenges.ContainsChallenge(timestampSignature) {
		return "", fmt.Errorf("disallowed reuse of ticket auth signature %v", timestampSignature)
	}

	authTimestamp, err := strconv.Atoi(timestamp)
	if err != nil {
		return "", fmt.Errorf("invalid ticket auth timestamp value %v", timestamp)
	}

	// Ensure that the auth timestamp is not more than
	// the permitted number of seconds into the past and future.
	timestampDelta := time.Now().Unix() - int64(authTimestamp)
	if math.Abs(float64(timestampDelta)) > maxTicketChallengeAge {
		return "", fmt.Errorf("expired or invalid ticket auth timestamp value %v", timestamp)
	}

	// confirm that the timestamp signature is a valid base64 string
	decodedSignature, err := base64.StdEncoding.DecodeString(timestampSignature)
	if err != nil {
		return "", fmt.Errorf("invalid ticket auth signature %s", timestampSignature)
	}

	// Mark this timestamp signature as used to prevent subsequent reuse.
	application.ProcessedTicketChallenges.AddChallenge(timestampSignature, maxTicketChallengeAge)

	// todo check if ticket belongs to this vsp

	// get user wallet address using ticket hash
	// todo: may be better to maintain a memory map of tickets-userWalletAddresses
	ticketInfo, err := application.StakepooldConnMan.GetTicketInfo(ticketHash)
	if err != nil {
		return "", fmt.Errorf("ticket auth, get ticket info failed: %v", err)
	}

	// Check if timestamp signature checks out against user's reward address.
	addr, err := dcrutil.DecodeAddress(ticketInfo.UserRewardAddress, application.Params)
	if err != nil {
		return "", fmt.Errorf("ticket auth, unexpected decode address error: %v", err)
	}

	valid, err := wallet.VerifyMessage(timestamp, addr, decodedSignature, application.Params)
	if err != nil {
		return "", fmt.Errorf("error validating timestamp signature for ticket auth %v", err)
	}

	if valid {
		return ticketInfo.MultiSigAddress, nil
	} else {
		return "", fmt.Errorf("invalid timestamp signature, timestamp was not signed using " +
			"the ticket owner's reward address")
	}
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
