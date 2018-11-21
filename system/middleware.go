package system

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/decred/dcrstakepool/models"
	"github.com/dgrijalva/jwt-go"
	"github.com/go-gorp/gorp"
	"github.com/gorilla/sessions"
	"github.com/zenazn/goji/web"
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
		session, _ := application.Store.Get(r, "session")
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
			c.Env["IsAPI"] = true
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				apitoken := strings.TrimPrefix(authHeader, "Bearer ")

				JWTtoken, err := jwt.Parse(apitoken, func(token *jwt.Token) (interface{}, error) {
					// validate signing algorithm
					if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
					}
					return []byte(application.APISecret), nil
				})

				if err != nil {
					log.Warnf("invalid token %v: %v", apitoken, err)
				} else {
					if claims, ok := JWTtoken.Claims.(jwt.MapClaims); ok && JWTtoken.Valid {
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
		} else {
			c.Env["IsAPI"] = false
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
		if userId, ok := session.Values["UserId"]; ok {
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

func (application *Application) ApplyIsXhr(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
			c.Env["IsXhr"] = true
		} else {
			c.Env["IsXhr"] = false
		}
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

func isValidToken(a, b string) bool {
	x := []byte(a)
	y := []byte(b)
	if len(x) != len(y) {
		return false
	}
	return subtle.ConstantTimeCompare(x, y) == 1
}

var csrfProtectionMethodForNoXhr = []string{"POST", "PUT", "DELETE"}

func isCsrfProtectionMethodForNoXhr(method string) bool {
	return strInSlice(csrfProtectionMethodForNoXhr, strings.ToUpper(method)) >= 0
}

func strInSlice(strs []string, str string) int {
	for i, v := range strs {
		if str == v {
			return i
		}
	}
	return -1
}

func (application *Application) ApplyCsrfProtection(c *web.C, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		// disable CSRF for API requests
		if c.Env["IsAPI"] != nil {
			if c.Env["IsAPI"].(bool) {
				h.ServeHTTP(w, r)
				return
			}
		} else {
			log.Error("IsAPI not set -- middleware not called in proper order")
		}
		session := c.Env["Session"].(*sessions.Session)
		csrfProtection := application.CsrfProtection
		if _, ok := session.Values["CsrfToken"]; !ok {
			hash := sha256.New()
			buffer := make([]byte, 32)
			_, err := rand.Read(buffer)
			if err != nil {
				log.Criticalf("crypt/rand.Read failed: %s", err)
				panic(err)
			}
			hash.Write(buffer)
			session.Values["CsrfToken"] = fmt.Sprintf("%x", hash.Sum(nil))
			if err = session.Save(r, w); err != nil {
				log.Criticalf("session.Save() failed")
				panic(err)
			}
		}
		c.Env["CsrfKey"] = csrfProtection.Key
		c.Env["CsrfToken"] = session.Values["CsrfToken"]
		csrfToken := c.Env["CsrfToken"].(string)

		if c.Env["IsXhr"].(bool) {
			if !isValidToken(csrfToken, r.Header.Get(csrfProtection.Header)) {
				http.Error(w, "Invalid Csrf Header", http.StatusBadRequest)
				return
			}
		} else {
			if isCsrfProtectionMethodForNoXhr(r.Method) {
				if !isValidToken(csrfToken, r.PostFormValue(csrfProtection.Key)) {
					http.Error(w, "Invalid Csrf Token", http.StatusBadRequest)
					return
				}
			}
		}
		http.SetCookie(w, &http.Cookie{
			Name:   csrfProtection.Cookie,
			Value:  csrfToken,
			Secure: csrfProtection.Secure,
			Path:   "/",
		})
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}
