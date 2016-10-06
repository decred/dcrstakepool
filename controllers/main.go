package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"html/template"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrutil/hdkeychain"
	"github.com/decred/dcrwallet/waddrmgr"

	"github.com/decred/dcrstakepool/helpers"
	"github.com/decred/dcrstakepool/models"
	"github.com/decred/dcrstakepool/system"
	"github.com/haisum/recaptcha"
	"github.com/zenazn/goji/web"
	"gopkg.in/gorp.v1"
)

// disapproveBlockMask
const disapproveBlockMask = 0x0000

// approveBlockMask
const approveBlockMask = 0x0001

// MainController
type MainController struct {
	system.Controller

	baseURL          string
	closePool        bool
	closePoolMsg     string
	extPub           *hdkeychain.ExtendedKey
	poolEmail        string
	poolFees         float64
	poolLink         string
	params           *chaincfg.Params
	rpcServers       *walletSvrManager
	recaptchaSecret  string
	recaptchaSiteKey string
	smtpFrom         string
	smtpHost         string
	smtpUsername     string
	smtpPassword     string
}

func randToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// NewMainController
func NewMainController(params *chaincfg.Params, baseURL string, closePool bool,
	closePoolMsg string, extPubStr string, poolEmail string, poolFees float64,
	poolLink string, recaptchaSecret string, recaptchaSiteKey string,
	smtpFrom string, smtpHost string, smtpUsername string, smtpPassword string,
	walletHosts []string, walletCerts []string, walletUsers []string,
	walletPasswords []string) (*MainController, error) {
	// Parse the extended public key and the pool fees.
	key, err := hdkeychain.NewKeyFromString(extPubStr)
	if err != nil {
		return nil, err
	}

	rpcs, err := newWalletSvrManager(walletHosts, walletCerts, walletUsers, walletPasswords)
	if err != nil {
		return nil, err
	}

	mc := &MainController{
		baseURL:          baseURL,
		closePool:        closePool,
		closePoolMsg:     closePoolMsg,
		extPub:           key,
		poolEmail:        poolEmail,
		poolFees:         poolFees,
		poolLink:         poolLink,
		params:           params,
		recaptchaSecret:  recaptchaSecret,
		recaptchaSiteKey: recaptchaSiteKey,
		rpcServers:       rpcs,
		smtpFrom:         smtpFrom,
		smtpHost:         smtpHost,
		smtpUsername:     smtpUsername,
		smtpPassword:     smtpPassword,
	}

	return mc, nil
}

// SendMail sends an email with the passed data using the system's SMTP
// configuration
func (controller *MainController) SendMail(emailaddress string, subject string, body string) error {

	hostname := controller.smtpHost

	if strings.Contains(controller.smtpHost, ":") {
		parts := strings.Split(controller.smtpHost, ":")
		hostname = parts[0]
	}

	// Set up authentication information.
	auth := smtp.PlainAuth("", controller.smtpUsername, controller.smtpPassword, hostname)

	// Connect to the server, authenticate, set the sender and recipient,
	// and send the email all in one step.
	to := []string{emailaddress}
	msg := []byte("To: " + emailaddress + "\r\n" +
		"From: " + controller.smtpFrom + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"\r\n" +
		body + "\r\n")

	if controller.smtpHost == "" {
		log.Warn("no mail server configured -- skipping sending " + string(msg))
		return nil
	}

	err := smtp.SendMail(controller.smtpHost, auth, controller.smtpFrom, to, msg)
	if err != nil {
		log.Errorf("Error sending email to %v", err)
	}
	return err
}

// FeeAddressForUserID generates a unique payout address per used ID for
// fees for an individual pool user.
func (controller *MainController) FeeAddressForUserID(uid int) (dcrutil.Address,
	error) {
	if uint32(uid+1) > waddrmgr.MaxAddressesPerAccount {
		return nil, fmt.Errorf("bad uid index %v", uid)
	}

	addrs, err := waddrmgr.AddressesDerivedFromExtPub(uint32(uid), uint32(uid+1),
		controller.extPub, waddrmgr.ExternalBranch, controller.params)
	if err != nil {
		return nil, err
	}

	return addrs[0], nil
}

// RPCStart
func (controller *MainController) RPCSync(dbMap *gorp.DbMap) error {
	multisigScripts, err := models.GetAllCurrentMultiSigScripts(dbMap)
	if err != nil {
		return err
	}
	err = walletSvrsSync(controller.rpcServers, multisigScripts)
	if err != nil {
		return err
	}
	return nil
}

// RPCStart
func (controller *MainController) RPCStart() {
	controller.rpcServers.Start()
}

// RPCStop
func (controller *MainController) RPCStop() error {
	return controller.rpcServers.Stop()
}

// RPCIsStopped
func (controller *MainController) RPCIsStopped() bool {
	return controller.rpcServers.IsStopped()
}

// handlePotentialFatalError
func (controller *MainController) handlePotentialFatalError(fn string, err error) {
	cnErr, ok := err.(connectionError)
	if ok {
		log.Infof("RPC %s failed on connection error: %v", fn, cnErr)
	}
	controller.RPCStop()
	log.Infof("RPC %s failed: %v", fn, err)
}

// Address page route
func (controller *MainController) Address(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	c.Env["IsAddress"] = true
	c.Env["Network"] = controller.params.Name

	//dbMap := controller.GetDbMap(c)
	//user := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	c.Env["Flash"] = session.Flashes("address")
	var widgets = controller.Parse(t, "address", c.Env)

	c.Env["Title"] = "Decred Stake Pool - Address"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Address form submit route
func (controller *MainController) AddressPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)

	// User may have a session so error out here as well
	if controller.closePool {
		session.AddFlash(controller.closePoolMsg, "address")
		return controller.Address(c, r)
	}

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	dbMap := controller.GetDbMap(c)
	user := models.GetUserById(dbMap, session.Values["UserId"].(int64))
	if len(user.UserPubKeyAddr) > 0 {
		session.AddFlash("Stake pool is currently limited to one address per account", "address")
		return controller.Address(c, r)
	}

	userPubKeyAddr := r.FormValue("UserPubKeyAddr")

	if len(userPubKeyAddr) < 40 {
		session.AddFlash("Address is too short", "address")
		return controller.Address(c, r)
	}

	if len(userPubKeyAddr) > 65 {
		session.AddFlash("Address is too long", "address")
		return controller.Address(c, r)
	}

	u, err := dcrutil.DecodeAddress(userPubKeyAddr, controller.params)
	if err != nil {
		session.AddFlash("Couldn't decode address", "address")
		return controller.Address(c, r)
	}

	_, is := u.(*dcrutil.AddressSecpPubKey)
	if !is {
		session.AddFlash("Incorrect address type", "address")
		return controller.Address(c, r)
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	pooladdress, err := controller.rpcServers.GetNewAddress()
	if err != nil {
		controller.handlePotentialFatalError("GetNewAddress", err)
		return "/error", http.StatusSeeOther
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	poolValidateAddress, err := controller.rpcServers.ValidateAddress(pooladdress)
	if err != nil {
		controller.handlePotentialFatalError("ValidateAddress pooladdress", err)
		return "/error", http.StatusSeeOther
	}
	poolPubKeyAddr := poolValidateAddress.PubKeyAddr

	p, err := dcrutil.DecodeAddress(poolPubKeyAddr, controller.params)
	if err != nil {
		controller.handlePotentialFatalError("DecodeAddress poolPubKeyAddr", err)
		return "/error", http.StatusSeeOther
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	createMultiSig, err := controller.rpcServers.CreateMultisig(1, []dcrutil.Address{p, u})
	if err != nil {
		controller.handlePotentialFatalError("CreateMultisig", err)
		return "/error", http.StatusSeeOther
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	_, bestBlockHeight, err := controller.rpcServers.GetBestBlock()
	if err != nil {
		controller.handlePotentialFatalError("GetBestBlock", err)
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	serializedScript, err := hex.DecodeString(createMultiSig.RedeemScript)
	if err != nil {
		controller.handlePotentialFatalError("CreateMultisig DecodeString", err)
		return "/error", http.StatusSeeOther
	}
	err = controller.rpcServers.ImportScript(serializedScript, int(bestBlockHeight))
	if err != nil {
		controller.handlePotentialFatalError("ImportScript", err)
		return "/error", http.StatusSeeOther
	}

	uid64 := session.Values["UserId"].(int64)
	userFeeAddr, err := controller.FeeAddressForUserID(int(uid64))
	if err != nil {
		log.Warnf("unexpected error deriving pool addr: %s", err.Error())
		return "/error", http.StatusSeeOther
	}

	models.UpdateUserById(dbMap, uid64, createMultiSig.Address,
		createMultiSig.RedeemScript, poolPubKeyAddr, userPubKeyAddr,
		userFeeAddr.EncodeAddress(), bestBlockHeight)

	return "/tickets", http.StatusSeeOther
}

// EmailUpdate validates the passed token and updates the user's email address
func (controller *MainController) EmailUpdate(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	// validate that the token is set, valid, and not expired
	token := r.URL.Query().Get("t")

	if token != "" {
		failed := false
		emailChange, err := helpers.EmailChangeTokenExists(dbMap, token)
		if err != nil {
			session.AddFlash("Email verification token not valid",
				"emailupdateError")
			failed = true
		}

		if !failed {
			if emailChange.Expires-time.Now().Unix() <= 0 {
				session.AddFlash("Email change token has expired",
					"emailupdateError")
				failed = true
			}
		}

		// possible that someone signed up with this email in the time between
		// when the token was generated and now.
		if !failed {
			userExists := models.GetUserByEmail(dbMap, emailChange.NewEmail)
			if userExists != nil {
				session.AddFlash("Email address is in use", "emailupdateError")
				failed = true
			}
		}

		if !failed {
			err := helpers.EmailChangeComplete(dbMap, token)
			if err != nil {
				session.AddFlash("Error occurred while changing email address",
					"emailupdateError")
				log.Errorf("EmailChangeComplete failed %v", err)
			} else {
				// Logout the user to force them to sign in with their new
				// email address
				session.Values["UserId"] = nil
				session.AddFlash("Email successfully updated",
					"emailupdateSuccess")
			}
		}
	} else {
		session.AddFlash("No email verification token present",
			"emailupdateError")
	}

	c.Env["FlashError"] = session.Flashes("emailupdateError")
	c.Env["FlashSuccess"] = session.Flashes("emailupdateSuccess")

	widgets := controller.Parse(t, "emailupdate", c.Env)
	c.Env["IsEmailUpdate"] = true
	c.Env["Title"] = "Decred Stake Pool - Email Update"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// EmailVerify renders the email verification page
func (controller *MainController) EmailVerify(c web.C, r *http.Request) (string,
	int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	// validate that the token is set and valid
	token := r.URL.Query().Get("t")

	if token != "" {
		_, err := helpers.EmailVerificationTokenExists(dbMap, token)
		if err != nil {
			session.AddFlash("Email verification token not valid",
				"emailverifyError")
		} else {
			err := helpers.EmailVerificationComplete(dbMap, token)
			if err != nil {
				session.AddFlash("Unable to set email to verified status",
					"emailverifyError")
				log.Errorf("could not set email to verified %v", err)
			} else {
				session.AddFlash("Email successfully verified",
					"emailverifySuccess")
			}
		}
	} else {
		session.AddFlash("No email verification token present",
			"emailverifyError")
	}

	c.Env["FlashError"] = session.Flashes("emailverifyError")
	c.Env["FlashSuccess"] = session.Flashes("emailverifySuccess")

	widgets := controller.Parse(t, "emailverify", c.Env)
	c.Env["IsEmailVerify"] = true
	c.Env["Title"] = "Decred Stake Pool - Email Verification"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Error page route
func (controller *MainController) Error(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)

	var rpcstatus = "Running"

	if controller.RPCIsStopped() {
		rpcstatus = "Stopped"
	}

	c.Env["IsError"] = true
	c.Env["Title"] = "Decred Stake Pool - Error"
	c.Env["RPCStatus"] = rpcstatus
	c.Env["RateLimited"] = r.URL.Query().Get("rl")
	c.Env["Referer"] = r.URL.Query().Get("r")

	var widgets = controller.Parse(t, "error", c.Env)
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Home page route
func (controller *MainController) Index(c web.C, r *http.Request) (string, int) {
	if controller.closePool {
		c.Env["IsClosed"] = true
		c.Env["ClosePoolMsg"] = controller.closePoolMsg
	}
	c.Env["Network"] = controller.params.Name
	c.Env["PoolEmail"] = controller.poolEmail
	c.Env["PoolFees"] = controller.poolFees
	c.Env["PoolLink"] = controller.poolLink

	t := controller.GetTemplate(c)

	widgets := helpers.Parse(t, "home", c.Env)

	c.Env["IsIndex"] = true

	c.Env["Title"] = "Decred Stake Pool - Welcome"
	c.Env["Content"] = template.HTML(widgets)

	return helpers.Parse(t, "main", c.Env), http.StatusOK
}

// PasswordReset renders the password reset page
func (controller *MainController) PasswordReset(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	c.Env["FlashError"] = session.Flashes("passwordresetError")
	c.Env["FlashSuccess"] = session.Flashes("passwordresetSuccess")
	c.Env["IsPasswordReset"] = true
	c.Env["RecaptchaSiteKey"] = controller.recaptchaSiteKey
	if controller.smtpHost == "" {
		c.Env["SMTPDisabled"] = true
	}

	widgets := controller.Parse(t, "passwordreset", c.Env)
	c.Env["Title"] = "Decred Stake Pool - Password Reset"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// PasswordResetPost handles the posted password reset form
func (controller *MainController) PasswordResetPost(c web.C, r *http.Request) (string, int) {
	email := r.FormValue("email")
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	re := recaptcha.R{
		Secret: controller.recaptchaSecret,
	}

	isValid := re.Verify(*r)
	if !isValid {
		log.Errorf("Recaptcha error %v", re.LastError())
		session.AddFlash("Recaptcha error", "passwordresetError")
		return controller.PasswordReset(c, r)
	}

	user, err := helpers.EmailExists(dbMap, email)

	if err != nil {
		session.AddFlash("Invalid Email", "passwordresetError")
	} else {
		t := time.Now()
		expires := t.Add(time.Hour * 1)

		token := randToken()
		passReset := &models.PasswordReset{
			UserId:  user.Id,
			Token:   token,
			Created: t.Unix(),
			Expires: expires.Unix(),
		}

		if err := models.InsertPasswordReset(dbMap, passReset); err != nil {
			session.AddFlash("Unable to add reset token to database", "passwordresetError")
			log.Errorf("Unable to add reset token to database: %v", err)
			return controller.PasswordReset(c, r)
		}

		remoteIP := r.RemoteAddr
		if strings.Contains(remoteIP, ":") {
			parts := strings.Split(remoteIP, ":")
			remoteIP = parts[0]
		}

		body := "A request to reset your password was made from IP address: " +
			remoteIP + "\r\n\n" +
			"If you made this request, follow the link below:\r\n\n" +
			controller.baseURL + "/passwordupdate?t=" + token + "\r\n\n" +
			"The above link expires an hour after this email was sent.\r\n\n" +
			"If you did not make this request, you may safely ignore this " +
			"email.\r\n" + "However, you may want to look into how this " +
			"happened.\r\n"
		err := controller.SendMail(user.Email, "Stake pool password reset", body)
		if err != nil {
			session.AddFlash("Unable to send email reset", "passwordresetError")
			log.Errorf("error sending password reset email %v", err)
		} else {
			session.AddFlash("Password reset email sent", "passwordresetSuccess")
		}
	}

	return controller.PasswordReset(c, r)
}

// PasswordUpdate renders the password update page
func (controller *MainController) PasswordUpdate(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	// validate that the token is set, valid, and not expired
	token := r.URL.Query().Get("t")

	if token != "" {
		passwordReset, err := helpers.PasswordResetTokenExists(dbMap, token)
		if err != nil {
			session.AddFlash("Password update token not valid", "passwordupdateError")
		} else {
			if passwordReset.Expires-time.Now().Unix() <= 0 {
				session.AddFlash("Password update token has expired", "passwordupdateError")
			}
		}
	} else {
		session.AddFlash("No password update token present", "passwordupdateError")
	}

	c.Env["FlashError"] = session.Flashes("passwordupdateError")
	c.Env["FlashSuccess"] = session.Flashes("passwordupdateSuccess")

	widgets := controller.Parse(t, "passwordupdate", c.Env)
	c.Env["IsPasswordUpdate"] = true
	c.Env["Title"] = "Decred Stake Pool - Password Update"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// PasswordUpdatePost handles updating passwords
func (controller *MainController) PasswordUpdatePost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	// validate that the token is set and not expired
	token := r.URL.Query().Get("t")

	if token == "" {
		session.AddFlash("No password update token present", "passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	passwordReset, err := helpers.PasswordResetTokenExists(dbMap, token)
	if err != nil {
		log.Errorf("error updating password %v", err)
		session.AddFlash("Password update token not valid",
			"passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	if passwordReset.Expires-time.Now().Unix() <= 0 {
		session.AddFlash("Password update token has expired",
			"passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	password, passwordRepeat := r.FormValue("password"),
		r.FormValue("passwordrepeat")
	if password == "" {
		session.AddFlash("Password cannot be empty", "passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	if password != passwordRepeat {
		session.AddFlash("Passwords do not match", "passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	user, err := helpers.UserIDExists(dbMap, passwordReset.UserId)
	if err != nil {
		log.Infof("UserIDExists failure %v", err)
		session.AddFlash("Unable to find User ID", "passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	user.HashPassword(password)
	_, err = helpers.UpdateUserPasswordById(dbMap, passwordReset.UserId,
		user.Password)
	if err != nil {
		log.Errorf("error updating password %v", err)
		session.AddFlash("Unable to update password", "passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	err = helpers.PasswordResetTokenDelete(dbMap, token)
	if err != nil {
		log.Errorf("error deleting token %v", err)
	}

	session.AddFlash("Password successfully updated", "passwordupdateSuccess")
	return controller.PasswordUpdate(c, r)
}

// Settings renders the settings page
func (controller *MainController) Settings(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	user := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	c.Env["FlashError"] = session.Flashes("settingsError")
	c.Env["FlashSuccess"] = session.Flashes("settingsSuccess")
	c.Env["CurrentEmail"] = user.Email
	c.Env["IsSettings"] = true
	if controller.smtpHost == "" {
		c.Env["SMTPDisabled"] = true
	}
	c.Env["RecaptchaSiteKey"] = controller.recaptchaSiteKey

	widgets := controller.Parse(t, "settings", c.Env)
	c.Env["Title"] = "Decred Stake Pool - Settings"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// SettingsPost handles changing the user's email address or password
func (controller *MainController) SettingsPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	password, updateEmail, updatePassword := r.FormValue("password"),
		r.FormValue("updateEmail"), r.FormValue("updatePassword")

	user, err := helpers.PasswordValidById(dbMap, session.Values["UserId"].(int64), password)
	if err != nil {
		session.AddFlash("Password not valid", "settingsError")
		return controller.Settings(c, r)
	}

	remoteIP := r.RemoteAddr
	if strings.Contains(remoteIP, ":") {
		parts := strings.Split(remoteIP, ":")
		remoteIP = parts[0]
	}

	if updateEmail == "true" {
		newEmail := r.FormValue("email")
		log.Infof("user requested email change from %v to %v", user.Email, newEmail)

		re := recaptcha.R{
			Secret: controller.recaptchaSecret,
		}

		isValid := re.Verify(*r)
		if !isValid {
			session.AddFlash("Recaptcha error", "settingsError")
			log.Errorf("Recaptcha error %v", re.LastError())
			return controller.Settings(c, r)
		}

		userExists := models.GetUserByEmail(dbMap, newEmail)

		if userExists != nil {
			session.AddFlash("Email address in use", "settingsError")
			return controller.Settings(c, r)
		}

		t := time.Now()
		expires := t.Add(time.Hour * 1)

		token := randToken()
		emailChange := &models.EmailChange{
			UserId:   user.Id,
			NewEmail: newEmail,
			Token:    token,
			Created:  t.Unix(),
			Expires:  expires.Unix(),
		}

		if err := models.InsertEmailChange(dbMap, emailChange); err != nil {
			session.AddFlash("Unable to add email change token to database", "settingsError")
			log.Errorf("Unable to add email change token to database: %v", err)
			return controller.Settings(c, r)
		}

		bodyNew := "A request was made to change the email address\r\n" +
			"for a stake pool account at " + controller.baseURL + "\r\n" +
			"from " + user.Email + " to " + newEmail + "\r\n\n" +
			"The request was made from IP address " + remoteIP + "\r\n\n" +
			"If you made this request, follow the link below:\r\n\n" +
			controller.baseURL + "/emailupdate?t=" + token + "\r\n\n" +
			"The above link expires an hour after this email was sent.\r\n\n" +
			"If you did not make this request, you may safely ignore this " +
			"email.\r\n" + "However, you may want to look into how this " +
			"happened.\r\n"
		err = controller.SendMail(newEmail, "Stake pool email change", bodyNew)
		if err != nil {
			session.AddFlash("Unable to send email change token.",
				"settingsError")
			log.Errorf("error sending email change token to new address %v %v",
				newEmail, err)
		} else {
			session.AddFlash("Verification token sent to new email address",
				"settingsSuccess")
		}

		bodyOld := "A request was made to change the email address\r\n" +
			"for your stake pool account at " + controller.baseURL + "\r\n" +
			"from " + user.Email + " to " + newEmail + "\r\n\n" +
			"The request was made from IP address " + remoteIP + "\r\n\n" +
			"If you did not make this request, please contact the \r\n" +
			"stake pool administrator immediately.\r\n"
		err = controller.SendMail(user.Email, "Stake pool email change",
			bodyOld)
		// this likely has the same status as the above email so don't
		// inform the user.
		if err != nil {
			log.Errorf("error sending email change token to old address %v %v",
				user.Email, err)
		}
	}

	if updatePassword == "true" {
		newPassword, newPasswordRepeat := r.FormValue("newpassword"),
			r.FormValue("newpasswordrepeat")
		if newPassword != newPasswordRepeat {
			session.AddFlash("Passwords do not match", "settingsError")
			return controller.Settings(c, r)
		}

		user.HashPassword(newPassword)
		_, err = helpers.UpdateUserPasswordById(dbMap, user.Id,
			user.Password)
		if err != nil {
			log.Errorf("error updating password %v", err)
			session.AddFlash("Unable to update password", "settingsError")
			return controller.PasswordUpdate(c, r)
		}

		// send a confirmation email
		body := "Your stake pool password for " + controller.baseURL + "\r\n" +
			"was just changed by IP Address " + remoteIP + "\r\n\n" +
			"If you did not make this request, please contact the \r\n" +
			"stake pool administrator immediately.\r\n"
		err = controller.SendMail(user.Email, "Stake pool password change",
			body)
		if err != nil {
			log.Errorf("error sending password change confirmation %v %v",
				user.Email, err)
		}

		session.AddFlash("Password successfully updated", "settingsSuccess")
	}

	return controller.Settings(c, r)
}

// Sign in route
func (controller *MainController) SignIn(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)

	c.Env["IsSignIn"] = true

	c.Env["Flash"] = session.Flashes("auth")
	var widgets = controller.Parse(t, "auth/signin", c.Env)

	c.Env["Title"] = "Decred Stake Pool - Sign In"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Sign In form submit route. Logs user in or sets an appropriate message in
// session if login was not successful
func (controller *MainController) SignInPost(c web.C, r *http.Request) (string, int) {
	email, password := r.FormValue("email"), r.FormValue("password")

	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	user, err := helpers.Login(dbMap, email, password)

	if err != nil {
		log.Infof(email+" login failed %v", err)
		session.AddFlash("Invalid Email or Password", "auth")
		return controller.SignIn(c, r)
	}

	if user.EmailVerified == 0 {
		session.AddFlash("You must validate your email address", "auth")
		return controller.SignIn(c, r)
	}

	if controller.closePool {
		if len(user.UserPubKeyAddr) == 0 {
			session.AddFlash(controller.closePoolMsg, "auth")
			c.Env["IsClosed"] = true
			c.Env["ClosePoolMsg"] = controller.closePoolMsg
			return controller.SignIn(c, r)
		}
	}

	session.Values["UserId"] = user.Id

	if user.MultiSigAddress == "" {
		return "/address", http.StatusSeeOther
	}

	return "/tickets", http.StatusSeeOther
}

// Sign up route
func (controller *MainController) SignUp(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)

	c.Env["IsSignUp"] = true
	if controller.smtpHost == "" {
		c.Env["SMTPDisabled"] = true
	}
	if controller.closePool {
		c.Env["IsClosed"] = true
		c.Env["ClosePoolMsg"] = controller.closePoolMsg
	}

	c.Env["FlashError"] = session.Flashes("signupError")
	c.Env["FlashSuccess"] = session.Flashes("signupSuccess")
	c.Env["RecaptchaSiteKey"] = controller.recaptchaSiteKey

	var widgets = controller.Parse(t, "auth/signup", c.Env)

	c.Env["Title"] = "Decred Stake Pool - Sign Up"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Sign Up form submit route. Registers new user or shows Sign Up route with appropriate messages set in session
func (controller *MainController) SignUpPost(c web.C, r *http.Request) (string, int) {
	if controller.closePool {
		log.Infof("attempt to signup while registration disabled")
		return "/error?r=/signup", http.StatusSeeOther
	}

	re := recaptcha.R{
		Secret: controller.recaptchaSecret,
	}

	session := controller.GetSession(c)

	email, password, passwordRepeat := r.FormValue("email"),
		r.FormValue("password"), r.FormValue("passwordrepeat")

	if password == "" {
		session.AddFlash("password cannot be empty", "signupError")
		return controller.PasswordUpdate(c, r)
	}

	if password != passwordRepeat {
		session.AddFlash("passwords do not match", "signupError")
		return controller.SignUp(c, r)
	}

	isValid := re.Verify(*r)
	if !isValid {
		session.AddFlash("Recaptcha error", "signupError")
		log.Errorf("Recaptcha error %v", re.LastError())
		return controller.SignUp(c, r)
	}

	dbMap := controller.GetDbMap(c)
	user := models.GetUserByEmail(dbMap, email)

	if user != nil {
		session.AddFlash("User exists", "signupError")
		return controller.SignUp(c, r)
	}

	token := randToken()
	user = &models.User{
		Username:      email,
		Email:         email,
		EmailToken:    token,
		EmailVerified: 0,
	}
	user.HashPassword(password)

	if err := models.InsertUser(dbMap, user); err != nil {
		session.AddFlash("Database error occurred while adding user", "signupError")
		log.Errorf("Error while registering user: %v", err)
		return controller.SignUp(c, r)
	}

	remoteIP := r.RemoteAddr
	if strings.Contains(remoteIP, ":") {
		parts := strings.Split(remoteIP, ":")
		remoteIP = parts[0]
	}

	body := "A request for an account for " + controller.baseURL + "\r\n" +
		"was made from " + remoteIP + " for this email address.\r\n\n" +
		"If you made this request, follow the link below:\r\n\n" +
		controller.baseURL + "/emailverify?t=" + token + "\r\n\n" +
		"to verify your email address and finalize registration.\r\n\n"

	err := controller.SendMail(user.Email, "Stake pool email verification", body)
	if err != nil {
		session.AddFlash("Unable to send signup email", "signupError")
		log.Errorf("error sending verification email %v", err)
	} else {
		session.AddFlash("A verification email has been sent to "+email, "signupSuccess")
	}

	//session.Values["UserId"] = user.Id
	//return "/address", http.StatusSeeOther
	return controller.SignUp(c, r)
}

// Stats page route
func (controller *MainController) Stats(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	c.Env["IsStats"] = true
	c.Env["Title"] = "Decred Stake Pool - Stats"

	dbMap := controller.GetDbMap(c)

	usercount, err := dbMap.SelectInt("SELECT COUNT(*) FROM Users")
	if err != nil {
		log.Infof("user count query failed")
		return "/error?r=/stats", http.StatusSeeOther
	}

	usercountactive, err := dbMap.SelectInt("SELECT COUNT(*) FROM Users WHERE MultiSigAddress <> ''")
	if err != nil {
		log.Infof("user count query failed")
		return "/error?r=/stats", http.StatusSeeOther
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	gsi, err := controller.rpcServers.GetStakeInfo()
	if err != nil {
		log.Infof("RPC GetStakeInfo failed: %v", err)
		return "/error?r=/stats", http.StatusSeeOther
	}

	c.Env["Network"] = controller.params.Name
	if controller.closePool {
		c.Env["PoolStatus"] = "Closed"
	} else {
		c.Env["PoolStatus"] = "Open"
	}
	c.Env["PoolEmail"] = controller.poolEmail
	c.Env["PoolFees"] = controller.poolFees
	c.Env["StakeInfo"] = gsi
	c.Env["UserCount"] = usercount
	c.Env["UserCountActive"] = usercountactive

	var widgets = controller.Parse(t, "stats", c.Env)
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Status page route
func (controller *MainController) Status(c web.C, r *http.Request) (string, int) {
	var rpcstatus = "Running"

	if controller.RPCIsStopped() {
		rpcstatus = "Stopped"
	}

	t := controller.GetTemplate(c)
	c.Env["IsStatus"] = true
	c.Env["Title"] = "Decred Stake Pool - Status"
	c.Env["RPCStatus"] = rpcstatus

	var widgets = controller.Parse(t, "status", c.Env)
	c.Env["Content"] = template.HTML(widgets)

	if controller.RPCIsStopped() {
		return controller.Parse(t, "main", c.Env), http.StatusInternalServerError
	}

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Tickets page route
func (controller *MainController) Tickets(c web.C, r *http.Request) (string, int) {
	type TicketInfoHistoric struct {
		Ticket        string
		SpentBy       string
		SpentByHeight uint32
		TicketHeight  uint32
	}

	type TicketInfoInvalid struct {
		Ticket string
	}

	type TicketInfoLive struct {
		Ticket       string
		TicketHeight uint32
		VoteBits     uint16
	}

	ticketInfoInvalid := map[int]TicketInfoInvalid{}
	ticketInfoLive := map[int]TicketInfoLive{}
	ticketInfoMissed := map[int]TicketInfoHistoric{}
	ticketInfoVoted := map[int]TicketInfoHistoric{}

	t := controller.GetTemplate(c)
	session := controller.GetSession(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	c.Env["IsTickets"] = true
	c.Env["Network"] = controller.params.Name
	c.Env["PoolFees"] = controller.poolFees
	c.Env["Title"] = "Decred Stake Pool - Tickets"

	dbMap := controller.GetDbMap(c)
	user := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	if user.MultiSigAddress == "" {
		c.Env["Error"] = "No multisig data has been generated"
		log.Info("Multisigaddress empty")
	}

	ms, err := dcrutil.DecodeAddress(user.MultiSigAddress, controller.params)
	if err != nil {
		c.Env["Error"] = "Invalid multisig data in database"
		log.Infof("Invalid address %v in database: %v", user.MultiSigAddress, err)
	}

	var widgets = controller.Parse(t, "tickets", c.Env)

	if err != nil {
		log.Info("err is set")
		c.Env["Content"] = template.HTML(widgets)
		widgets = controller.Parse(t, "tickets", c.Env)
		return controller.Parse(t, "main", c.Env), http.StatusOK
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}

	spui := new(dcrjson.StakePoolUserInfoResult)
	spui, err = controller.rpcServers.StakePoolUserInfo(ms)
	if err != nil {
		// Log the error, but do not return. Consider reporting
		// the error to the user on the page. A blank tickets
		// page will be displayed in the meantime.
		log.Infof("RPC StakePoolUserInfo failed: %v", err)
	}

	if spui != nil && len(spui.Tickets) > 0 {
		var tickethashes []*chainhash.Hash

		for _, ticket := range spui.Tickets {
			th, err := chainhash.NewHashFromStr(ticket.Ticket)
			if err != nil {
				log.Infof("NewHashFromStr failed for %v", ticket)
				return "/error?r=/tickets", http.StatusSeeOther
			}
			tickethashes = append(tickethashes, th)
		}

		// TODO: only get votebits for live tickets
		gtvb, err := controller.rpcServers.GetTicketsVoteBits(tickethashes)
		if err != nil {
			log.Infof("GetTicketsVoteBits failed %v", err)
			return "/error?r=/tickets", http.StatusSeeOther
		}

		for idx, ticket := range spui.Tickets {
			switch {
			case ticket.Status == "live":
				ticketInfoLive[idx] = TicketInfoLive{
					Ticket:       ticket.Ticket,
					TicketHeight: ticket.TicketHeight,
					VoteBits:     gtvb.VoteBitsList[idx].VoteBits,
				}
			case ticket.Status == "missed":
				ticketInfoMissed[idx] = TicketInfoHistoric{
					Ticket:        ticket.Ticket,
					SpentByHeight: ticket.SpentByHeight,
					TicketHeight:  ticket.TicketHeight,
				}
			case ticket.Status == "voted":
				ticketInfoVoted[idx] = TicketInfoHistoric{
					Ticket:        ticket.Ticket,
					SpentBy:       ticket.SpentBy,
					SpentByHeight: ticket.SpentByHeight,
					TicketHeight:  ticket.TicketHeight,
				}
			}
		}

		for idx, ticket := range spui.InvalidTickets {
			ticketInfoInvalid[idx] = TicketInfoInvalid{ticket}
		}
	}

	c.Env["TicketsInvalid"] = ticketInfoInvalid
	c.Env["TicketsLive"] = ticketInfoLive
	c.Env["TicketsMissed"] = ticketInfoMissed
	c.Env["TicketsVoted"] = ticketInfoVoted
	widgets = controller.Parse(t, "tickets", c.Env)
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Tickets form submit route
func (controller *MainController) TicketsPost(c web.C, r *http.Request) (string, int) {
	chooseallow := r.FormValue("chooseallow")
	// votebitsmanual := r.FormValue("votebitsmanual")
	var voteBits = uint16(0)

	if chooseallow == "2" {
		// pool policy and approve
		// TODO: set policy somewhere else and make it available to /tickets page
		voteBits = uint16(1)
		voteBits |= approveBlockMask
	} else {
		if chooseallow == "1" {
			voteBits = approveBlockMask
		} else {
			voteBits = disapproveBlockMask
		}
	}

	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)
	user := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	if user.MultiSigAddress == "" {
		log.Info("Multisigaddress empty")
		return "/error?r=/tickets", http.StatusSeeOther
	}

	ms, err := dcrutil.DecodeAddress(user.MultiSigAddress, controller.params)
	if err != nil {
		log.Infof("Invalid address %v in database: %v", user.MultiSigAddress, err)
		return "/error?r=/tickets", http.StatusSeeOther
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	spui, err := controller.rpcServers.StakePoolUserInfo(ms)
	if err != nil {
		log.Infof("RPC StakePoolUserInfo failed: %v", err)
		return "/error?r=/tickets", http.StatusSeeOther
	}

	for _, ticket := range spui.Tickets {
		if controller.RPCIsStopped() {
			return "/error", http.StatusSeeOther
		}
		th, err := chainhash.NewHashFromStr(ticket.Ticket)
		if err != nil {
			log.Infof("NewHashFromStr failed for %v", ticket)
			return "/error?r=/tickets", http.StatusSeeOther
		}
		err = controller.rpcServers.SetTicketVoteBits(th, voteBits)
		if err != nil {
			if err == ErrSetVoteBitsCoolDown {
				return "/error?r=/tickets&rl=1", http.StatusSeeOther
			}
			controller.handlePotentialFatalError("SetTicketVoteBits", err)
			return "/error?r=/tickets", http.StatusSeeOther
		}
	}

	return "/tickets", http.StatusSeeOther
}

// This route logs user out
func (controller *MainController) Logout(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)

	session.Values["UserId"] = nil

	return "/", http.StatusSeeOther
}
