package system

import (
	"bytes"
	"html/template"
	"time"

	"github.com/decred/dcrstakepool/helpers"
	"github.com/decred/dcrstakepool/models"
	"github.com/go-gorp/gorp"
	"github.com/gorilla/sessions"
	"github.com/zenazn/goji/web"
)

type Controller struct {
}

func (controller *Controller) GetSession(c web.C) *sessions.Session {
	return c.Env["Session"].(*sessions.Session)
}

func (controller *Controller) GetTemplate(c web.C) *template.Template {
	return c.Env["Template"].(*template.Template)
}

func (controller *Controller) GetDbMap(c web.C) *gorp.DbMap {
	return c.Env["DbMap"].(*gorp.DbMap)
}

func (controller *Controller) IsCaptchaDone(c web.C) bool {
	done, ok := c.Env["CaptchaDone"].(bool)
	return done && ok
}

func (controller *Controller) Parse(t *template.Template, name string, data interface{}) string {
	var doc bytes.Buffer
	err := t.ExecuteTemplate(&doc, name, data)
	if err != nil {
		log.Warnf("ExecuteTemplate error: %v", err)
	}
	return doc.String()
}

// CheckPasswordResetToken checks that the input token string is valid,
// recognized by the DB, and not expired. Flash messages are added to the
// session for any token failure, and the return indicates if all checks have
// passed. The UserToken and PasswordReset objects are also returned.
func (controller *Controller) CheckPasswordResetToken(tokenStr string, c web.C) (models.UserToken, *models.PasswordReset, bool) {
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	var token models.UserToken

	// Check that the token is set.
	if tokenStr == "" {
		session.AddFlash("No password update token present.",
			"passwordupdateError")
		return token, nil, false
	}

	// Check that the token is valid.
	var err error
	token, err = models.UserTokenFromStr(tokenStr)
	if err != nil {
		session.AddFlash("Email verification token not valid.",
			"passwordupdateError")
		return token, nil, false
	}

	// Check that the token is recognized.
	passwordReset, err := helpers.PasswordResetTokenExists(dbMap, token)
	if err != nil {
		log.Debugf(`Password update token "%v" not found in DB: %v`, token, err)
		session.AddFlash("Password update token not recognized.",
			"passwordupdateError")
		return token, nil, false
	}

	// Check that the token is not expired.
	expTime := time.Unix(passwordReset.Expires, 0)
	if expTime.Before(time.Now()) {
		session.AddFlash("Password update token has expired.",
			"passwordupdateError")
		return token, passwordReset, false
	}

	return token, passwordReset, true
}
