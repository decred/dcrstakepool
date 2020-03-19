package system

import (
	"net/http"
	"strings"
	"time"

	"github.com/decred/dcrstakepool/models"
	"github.com/go-gorp/gorp"
	"github.com/gorilla/sessions"
	"github.com/zenazn/goji/web"
	gojimw "github.com/zenazn/goji/web/middleware"
	"github.com/zenazn/goji/web/mutil"
)

// ApplyTemplates makes sure templates are stored in the context
func (application *Application) ApplyTemplates(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		c.Env["Template"] = application.Template
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// ApplySessions makes sure controllers can have access to session
func (application *Application) ApplySessions(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		session, err := application.Store.New(r, "session")
		if err != nil {
			log.Warnf("session load err: %v ", err)
		}
		c.Env["Session"] = session
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// ApplyDbMap makes sure controllers can have access to the gorp DbMap.
func (application *Application) ApplyDbMap(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		c.Env["DbMap"] = application.DbMap
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// ApplyAPI verifies the header's API token and ensures it belongs to a user.
func (application *Application) ApplyAPI(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			var user *models.User
			var err error
			dbMap := c.Env["DbMap"].(*gorp.DbMap)

			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				var userId int64
				userId, err = application.validateToken(authHeader)
				if err == nil {
					user, err = models.GetUserByID(dbMap, userId)
				}
			} else if strings.HasPrefix(authHeader, "TicketAuth ") {
				var userMsa string
				userMsa, err = application.validateTicketOwnership(r.Context(), authHeader)
				if err == nil {
					user, err = models.GetUserByMSA(dbMap, userMsa)
				}
			}

			if err != nil {
				log.Warnf("api authorization failure: %v", err)
				c.Env["AuthErrorMessage"] = err.Error()
			} else if user != nil {
				c.Env["APIUserID"] = user.ID
				log.Infof("mapped api auth header %v to user %v", authHeader, user.ID)
			}
		}

		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// ApplyCaptcha verfies whether or not the captcha has been solved.
func (application *Application) ApplyCaptcha(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		session := c.Env["Session"].(*sessions.Session)
		if done, ok := session.Values["CaptchaDone"].(bool); ok {
			c.Env["CaptchaDone"] = done
		} else {
			c.Env["CaptchaDone"] = false
		}
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// ApplyAuth populates a user's info in the header if their userID is found in
// the database.
func (application *Application) ApplyAuth(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		session := c.Env["Session"].(*sessions.Session)
		if userID := session.Values["UserId"]; userID != nil {
			dbMap := c.Env["DbMap"].(*gorp.DbMap)

			user, err := dbMap.Get(models.User{}, userID)
			if err != nil {
				log.Warnf("Auth error: %v", err)
				c.Env["User"] = nil
			} else {
				c.Env["User"] = user
			}
		}
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// Logger is a middleware that logs the start and end of each request, along
// with some useful data about what was requested, what the response status was,
// and how long it took to return. This should be used after the RequestID
// middleware.
func Logger(RealIPHeader string) func(c *web.C, h http.Handler) http.Handler {
	return func(c *web.C, h http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			reqID := gojimw.GetReqID(*c)

			log.Tracef("[%s] Started %s %q, from %s", reqID, r.Method,
				r.URL.String(), ClientIP(r, RealIPHeader))

			lw := mutil.WrapWriter(w)

			t1 := time.Now()
			h.ServeHTTP(lw, r)

			if lw.Status() == 0 {
				lw.WriteHeader(http.StatusOK)
			}
			t2 := time.Now()

			log.Tracef("[%s] Returning %03d in %s", reqID, lw.Status(), t2.Sub(t1))
		}
		return http.HandlerFunc(fn)
	}
}
