package v3api

import (
	"net/http"
	"strings"

	"github.com/decred/dcrstakepool/models"
	"github.com/go-gorp/gorp"
	"github.com/zenazn/goji/web"
)

func (v3Api *V3API) ApplyTicketAuth(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v3") {
			authHeader := r.Header.Get("Authorization")
			msa := v3Api.validateTicketOwnership(authHeader)
			if msa != "" {
				dbMap := c.Env["DbMap"].(*gorp.DbMap)

				user, err := models.GetUserByMSA(dbMap, msa)
				if err != nil {
					log.Errorf("unable to map ticket auth %v with multisig address %v to user",
						authHeader, msa)
				} else {
					c.Env["APIUserID"] = user.Id
					log.Infof("mapped ticket auth %v to user id %v", authHeader, user.Id)
				}
			}
		}
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}
