package system

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/decred/dcrstakepool/models"
	"github.com/dgrijalva/jwt-go"
	"github.com/go-gorp/gorp"
	"github.com/gorilla/sessions"
	"github.com/zenazn/goji/web"
	gojimw "github.com/zenazn/goji/web/middleware"
	"github.com/zenazn/goji/web/mutil"
)

// Makes sure templates are stored in the context
func (application *Application) ApplyTemplates(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		c.Env["Template"] = application.Template
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// Makes sure controllers can have access to session
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

func (application *Application) ApplyDbMap(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		c.Env["DbMap"] = application.DbMap
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

func (application *Application) ApplyAPI(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				apitoken := strings.TrimPrefix(authHeader, "Bearer ")

				JWTtoken, err := jwt.Parse(apitoken, func(token *jwt.Token) (interface{}, error) {
					// validate signing algorithm
					if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
					}
					return []byte(application.APISecret), nil
				})

				if err != nil {
					log.Warnf("invalid token %v: %v", apitoken, err)
				} else if claims, ok := JWTtoken.Claims.(jwt.MapClaims); ok && JWTtoken.Valid {
					dbMap := c.Env["DbMap"].(*gorp.DbMap)

					user, err := models.GetUserById(dbMap, int64(claims["loggedInAs"].(float64)))
					if err != nil {
						log.Errorf("unable to map apitoken %v to user id %v", apitoken, claims["loggedInAs"])
					} else {
						c.Env["APIUserID"] = user.Id
						log.Infof("mapped apitoken %v to user id %v", apitoken, user.Id)
					}
				}
			}
		}
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

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

func (application *Application) ApplyAuth(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		session := c.Env["Session"].(*sessions.Session)
		if userId := session.Values["UserId"]; userId != nil {
			dbMap := c.Env["DbMap"].(*gorp.DbMap)

			user, err := dbMap.Get(models.User{}, userId)
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
