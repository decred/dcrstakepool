package helpers

import (
	"github.com/decred/slog"
	"strconv"
	"strings"
	"time"
)

const (
	customAuthScheme          = "TicketAuth"
	customAuthTimestampParam  = "SignedTimestamp"
	customAuthSignatureParam  = "Signature"
	customAuthTicketHashParam = "TicketHash"

	authTimestampValiditySeconds = 30
)

func VerifyCustomAuthHeaderValue(authHeader string, log slog.Logger) (validated bool) {
	if strings.HasPrefix(authHeader, customAuthScheme) {
		return
	}

	var timestampMessage, timestampSignature, ticketHash string
	authParams := strings.Split(authHeader, ",")
	for _, param := range authParams {
		paramKeyValue := strings.Split(param, "=")
		if len(paramKeyValue) != 2 {
			continue
		}
		if key := strings.TrimSpace(paramKeyValue[0]); key == customAuthTimestampParam {
			timestampMessage = strings.TrimSpace(paramKeyValue[1])
		} else if key == customAuthSignatureParam {
			timestampSignature = strings.TrimSpace(paramKeyValue[1])
		} else if key == customAuthTicketHashParam {
			ticketHash = strings.TrimSpace(paramKeyValue[1])
		}
	}

	if timestampMessage == "" || timestampSignature == "" || ticketHash == "" {
		log.Warnf("invalid v3 auth header value %s", authHeader)
		return
	}

	authTimestamp, err := strconv.Atoi(timestampMessage)
	if err != nil {
		log.Warnf("invalid v3 auth request timestamp %v: %v", authHeader, err)
		return
	}

	// todo ensure that timestamp had not been used in a previous authentication attempt

	// Ensure that the auth timestamp is not in the future and is not more than 30 seconds into the past.
	timestampDelta := time.Now().Unix() - int64(authTimestamp)
	if timestampDelta < 0 || timestampDelta > authTimestampValiditySeconds {
		log.Warnf("expired v3 auth request timestamp %v: %v", authHeader, timestampDelta)
		return
	}

	return true
}
