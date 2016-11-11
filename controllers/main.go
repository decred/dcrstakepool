package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrutil/hdkeychain"
	"github.com/decred/dcrwallet/waddrmgr"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrstakepool/helpers"
	"github.com/decred/dcrstakepool/models"
	"github.com/decred/dcrstakepool/system"
	"github.com/haisum/recaptcha"
	"github.com/zenazn/goji/web"
	"gopkg.in/gorp.v1"
)

// disapproveBlockMask checks to see if the votebits have been set to No.
const disapproveBlockMask = 0x0000

// approveBlockMask checks to see if votebits have been set to Yes.
const approveBlockMask = 0x0001

const signupEmailTemplate = "A request for an account for __URL__\r\n" +
	"was made from __REMOTEIP__ for this email address.\r\n\n" +
	"If you made this request, follow the link below:\r\n\n" +
	"__URL__/emailverify?t=__TOKEN__\r\n\n" +
	"to verify your email address and finalize registration.\r\n\n"
const signupEmailSubject = "Stake pool email verification"

// MainController is the wallet RPC controller type.  Its methods include the
// route handlers.
type MainController struct {
	// embed type for c.Env[""] context and ExecuteTemplate helpers
	system.Controller

	adminIPs         []string
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
	version          string
}

func randToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// NewMainController is the constructor for the entire controller routing.
func NewMainController(params *chaincfg.Params, adminIPs []string, baseURL string, closePool bool,
	closePoolMsg string, extPubStr string, poolEmail string, poolFees float64,
	poolLink string, recaptchaSecret string, recaptchaSiteKey string,
	smtpFrom string, smtpHost string, smtpUsername string, smtpPassword string,
	version string, walletHosts []string, walletCerts []string,
	walletUsers []string, walletPasswords []string, minServers int) (*MainController, error) {
	// Parse the extended public key and the pool fees.
	key, err := hdkeychain.NewKeyFromString(extPubStr)
	if err != nil {
		return nil, err
	}

	rpcs, err := newWalletSvrManager(walletHosts, walletCerts, walletUsers, walletPasswords, minServers)
	if err != nil {
		return nil, err
	}

	mc := &MainController{
		adminIPs:         adminIPs,
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
		version:          version,
	}

	return mc, nil
}

// API is the main frontend that handles all API requests.
// XXX This is very hacky.  It can be made more elegant by improving or
// re-writing the middleware and switching to a more modern framework
// such as goji2 or something else that utilizes the context package/built-in.
// This would make it much easier to share code between the web UI and JSON
// API with less duplication.
func (controller *MainController) API(c web.C, r *http.Request) (string, int) {
	// we expect /api/vX.YZ/command
	URIparts := strings.Split(r.RequestURI, "/")
	if len(URIparts) != 4 {
		return "{\"status\": \"error\"," +
			"\"message\": \"invalid API request\"}\n", http.StatusOK
	}
	version := URIparts[2]
	command := URIparts[3]

	if version != "v0.1" {
		return "{\"status\": \"error\"," +
			"\"message\": \"invalid API version\"}\n", http.StatusOK
	}

	switch r.Method {
	case "GET":
		switch command {
		case "getPurchaseInfo":
			data, response, err := controller.APIPurchaseInfo(c, r)
			return APIResponse(data, response, err), http.StatusOK
		case "startsession":
			return APIResponse(nil, "session started", nil), http.StatusOK
		case "stats":
			data, response, err := controller.APIStats(c, r)
			return APIResponse(data, response, err), http.StatusOK
		}
	case "POST":
		switch command {
		case "address":
			_, response, err := controller.APIAddress(c, r)
			return APIResponse(nil, response, err), http.StatusOK
		case "signin":
			_, response, err := controller.APISignIn(c, r)
			return APIResponse(nil, response, err), http.StatusOK
		case "signup":
			_, response, err := controller.APISignUp(c, r)
			return APIResponse(nil, response, err), http.StatusOK
		}
	}

	return "{\"status\": \"error\"," +
		"\"message\": \"invalid API command\"}\n", http.StatusOK
}

// APIAddress is AddressPost API'd a bit
func (controller *MainController) APIAddress(c web.C, r *http.Request) ([]string, string, error) {
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	if session.Values["UserId"] == nil {
		return nil, "address error", errors.New("invalid session")
	}

	user := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	// User may have a session so error out here as well
	if controller.closePool {
		return nil, "pool is closed", errors.New(controller.closePoolMsg)
	}

	if session.Values["UserId"] == nil {
		return nil, "address error", errors.New("invalid session")
	}

	if len(user.UserPubKeyAddr) > 0 {
		return nil, "address error", errors.New("address already submitted")
	}

	userPubKeyAddr := r.FormValue("UserPubKeyAddr")

	if len(userPubKeyAddr) < 40 {
		return nil, "address error", errors.New("address too short")
	}

	if len(userPubKeyAddr) > 65 {
		return nil, "address error", errors.New("address too long")
	}

	u, err := dcrutil.DecodeAddress(userPubKeyAddr, controller.params)
	if err != nil {
		return nil, "address error", errors.New("couldn't decode address")
	}

	_, is := u.(*dcrutil.AddressSecpPubKey)
	if !is {
		return nil, "address error", errors.New("incorrect address type")
	}

	if controller.RPCIsStopped() {
		return nil, "system error", errors.New("unable to process wallet commands")
	}
	pooladdress, err := controller.rpcServers.GetNewAddress()
	if err != nil {
		controller.handlePotentialFatalError("GetNewAddress", err)
		return nil, "system error", errors.New("unable to process wallet commands")
	}

	if controller.RPCIsStopped() {
		return nil, "system error", errors.New("unable to process wallet commands")
	}
	poolValidateAddress, err := controller.rpcServers.ValidateAddress(pooladdress)
	if err != nil {
		controller.handlePotentialFatalError("ValidateAddress pooladdress", err)
		return nil, "system error", errors.New("unable to process wallet commands")
	}
	poolPubKeyAddr := poolValidateAddress.PubKeyAddr

	p, err := dcrutil.DecodeAddress(poolPubKeyAddr, controller.params)
	if err != nil {
		controller.handlePotentialFatalError("DecodeAddress poolPubKeyAddr", err)
		return nil, "system error", errors.New("unable to process wallet commands")
	}

	if controller.RPCIsStopped() {
		return nil, "system error", errors.New("unable to process wallet commands")
	}
	createMultiSig, err := controller.rpcServers.CreateMultisig(1, []dcrutil.Address{p, u})
	if err != nil {
		controller.handlePotentialFatalError("CreateMultisig", err)
		return nil, "system error", errors.New("unable to process wallet commands")
	}

	if controller.RPCIsStopped() {
		return nil, "system error", errors.New("unable to process wallet commands")
	}
	_, bestBlockHeight, err := controller.rpcServers.GetBestBlock()
	if err != nil {
		controller.handlePotentialFatalError("GetBestBlock", err)
	}

	if controller.RPCIsStopped() {
		return nil, "system error", errors.New("unable to process wallet commands")
	}
	serializedScript, err := hex.DecodeString(createMultiSig.RedeemScript)
	if err != nil {
		controller.handlePotentialFatalError("CreateMultisig DecodeString", err)
		return nil, "system error", errors.New("unable to process wallet commands")
	}
	err = controller.rpcServers.ImportScript(serializedScript, int(bestBlockHeight))
	if err != nil {
		controller.handlePotentialFatalError("ImportScript", err)
		return nil, "system error", errors.New("unable to process wallet commands")
	}

	uid64 := session.Values["UserId"].(int64)
	userFeeAddr, err := controller.FeeAddressForUserID(int(uid64))
	if err != nil {
		log.Warnf("unexpected error deriving pool addr: %s", err.Error())
		return nil, "system error", errors.New("unable to process wallet commands")
	}

	models.UpdateUserByID(dbMap, uid64, createMultiSig.Address,
		createMultiSig.RedeemScript, poolPubKeyAddr, userPubKeyAddr,
		userFeeAddr.EncodeAddress(), bestBlockHeight)

	return nil, "address successfully imported", nil
}

// APIResponse formats a response
func APIResponse(data []string, response string, err error) string {
	if err != nil {
		return "{\"status\":\"error\"," +
			"\"message\":\"" + response + " - " + err.Error() + "\"}\n"
	}

	successResp := "{\"status\":\"success\"," +
		"\"message\":\"" + response + "\""

	if data != nil {
		// append the key+value pairs in data
		successResp = successResp + ",\"data\":{"
		for i := 0; i < len(data)-1; i = i + 2 {
			successResp = successResp + "\"" + data[i] + "\":" + "\"" + data[i+1] + "\","
		}
		successResp = strings.TrimSuffix(successResp, ",") + "}"
	}

	successResp = successResp + "}\n"

	return successResp
}

// APIPurchaseInfo fetches and returns the user's info or an error
func (controller *MainController) APIPurchaseInfo(c web.C, r *http.Request) ([]string, string, error) {
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	var purchaseInfo []string

	if session.Values["UserId"] == nil {
		return nil, "purchaseinfo error", errors.New("invalid session")
	}

	user := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	if len(user.UserPubKeyAddr) == 0 {
		return nil, "purchaseinfo error", errors.New("no address submitted")
	}

	purchaseInfo = append(purchaseInfo,
		"pooladdress", user.UserFeeAddr,
		"poolfees", strconv.FormatFloat(controller.poolFees, 'f', 2, 64),
		"script", user.MultiSigScript,
		"ticketaddress", user.MultiSigAddress,
	)

	return purchaseInfo, "purchaseinfo successfully retrieved", nil
}

// APISignIn is SignInPost API'd a bit
func (controller *MainController) APISignIn(c web.C, r *http.Request) ([]string, string, error) {
	email, password := r.FormValue("email"), r.FormValue("password")

	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	user, err := helpers.Login(dbMap, email, password)

	if err != nil {
		log.Infof("error logging in with email %v password %v err %v", email, password, err)
		return nil, "auth error", errors.New("invalid email or password")
	}

	if user.EmailVerified == 0 {
		return nil, "auth error", errors.New("you must validate your email address")
	}

	if controller.closePool {
		if len(user.UserPubKeyAddr) == 0 {
			return nil, "pool is closed", errors.New(controller.closePoolMsg)
		}
	}

	session.Values["UserId"] = user.Id

	return nil, "successfully signed in", nil
}

// APISignUp is SignUpPost API'd a bit
func (controller *MainController) APISignUp(c web.C, r *http.Request) ([]string, string, error) {
	if controller.closePool {
		log.Infof("attempt to signup while registration disabled")
		return nil, "pool is closed", errors.New(controller.closePoolMsg)
	}

	email, password, passwordRepeat := r.FormValue("email"),
		r.FormValue("password"), r.FormValue("passwordrepeat")

	if !strings.Contains(email, "@") {
		return nil, "signup error", errors.New("email address is invalid")
	}

	if password == "" {
		return nil, "signup error", errors.New("password cannot be empty")
	}

	if password != passwordRepeat {
		return nil, "signup error", errors.New("passwords do not match")
	}

	dbMap := controller.GetDbMap(c)
	user := models.GetUserByEmail(dbMap, email)

	if user != nil {
		return nil, "signup error", errors.New("email address already in use")
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
		return nil, "signup error", errors.New("unable to insert new user into db")
	}

	remoteIP := r.RemoteAddr
	if strings.Contains(remoteIP, ":") {
		parts := strings.Split(remoteIP, ":")
		remoteIP = parts[0]
	}

	body := signupEmailTemplate
	body = strings.Replace(body, "__URL__", controller.baseURL, -1)
	body = strings.Replace(body, "__REMOTEIP__", remoteIP, -1)
	body = strings.Replace(body, "__TOKEN__", token, -1)

	err := controller.SendMail(user.Email, signupEmailSubject, body)
	if err != nil {
		log.Errorf("error sending verification email %v", err)
		return nil, "signup error", errors.New("unable to send signup email")
	}

	return nil, "A verification email has been sent to " + email, nil
}

// APIStats fetches is Stats() API'd a bit
func (controller *MainController) APIStats(c web.C, r *http.Request) ([]string, string, error) {
	var stats []string

	dbMap := controller.GetDbMap(c)
	userCount := models.GetUserCount(dbMap)
	userCountActive := models.GetUserCountActive(dbMap)

	if controller.RPCIsStopped() {
		return nil, "stats error", errors.New("RPC server stopped")
	}
	gsi, err := controller.rpcServers.GetStakeInfo()
	if err != nil {
		log.Infof("RPC GetStakeInfo failed: %v", err)
		return nil, "stats error", errors.New("RPC server error")
	}

	poolStatus := "Unknown"
	if controller.closePool {
		poolStatus = "Closed"
	} else {
		poolStatus = "Open"
	}

	stats = append(stats,
		"AllMempoolTix", fmt.Sprintf("%d", gsi.AllMempoolTix),
		"BlockHeight", fmt.Sprintf("%d", gsi.BlockHeight),
		"Difficulty", fmt.Sprintf("%f", gsi.Difficulty),
		"Immature", fmt.Sprintf("%d", gsi.Immature),
		"Live", fmt.Sprintf("%d", gsi.Live),
		"Missed", fmt.Sprintf("%d", gsi.Missed),
		"OwnMempoolTix", fmt.Sprintf("%d", gsi.OwnMempoolTix),
		"PoolSize", fmt.Sprintf("%d", gsi.PoolSize),
		"ProportionLive", fmt.Sprintf("%f", gsi.ProportionLive),
		"ProportionMissed", fmt.Sprintf("%f", gsi.ProportionMissed),
		"Revoked", fmt.Sprintf("%d", gsi.Revoked),
		"TotalSubsidy", fmt.Sprintf("%f", gsi.TotalSubsidy),
		"Voted", fmt.Sprintf("%d", gsi.Voted),
		"Network", controller.params.Name,
		"PoolEmail", controller.poolEmail,
		"PoolFees", strconv.FormatFloat(controller.poolFees, 'f', 2, 64),
		"PoolStatus", poolStatus,
		"UserCount", fmt.Sprintf("%d", userCount),
		"UserCountActive", fmt.Sprintf("%d", userCountActive),
		"Version", controller.version,
	)

	return stats, "stats successfully retrieved", nil
}

// SendMail sends an email with the passed data using the system's SMTP
// configuration.
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

// RPCSync checks to ensure that the wallets are synced on startup.
func (controller *MainController) RPCSync(dbMap *gorp.DbMap) error {
	multisigScripts, err := models.GetAllCurrentMultiSigScripts(dbMap)
	if err != nil {
		return err
	}

	err = walletSvrsSync(controller.rpcServers, multisigScripts)
	if err != nil {
		return err
	}

	// TODO: Wait for wallets to sync, or schedule the vote bits sync somehow.
	// For now, just skip full vote bits sync in favor of on-demand user's vote
	// bits sync if the wallets are busy at this point.

	// Allow sync to get going before attempting vote bits sync.
	time.Sleep(2 * time.Second)

	// Look for that -4 message from wallet that says: "the wallet is
	// currently syncing to the best block, please try again later"
	err = controller.rpcServers.CheckWalletsReady()
	if err != nil /*strings.Contains(err.Error(), "try again later")*/ {
		// If importscript is running, it will take a while.
		log.Errorf("Wallets are syncing. Unable to initiate votebits sync: %v",
			err)
	} else {
		// Sync vote bits for all tickets owned by the wallet
		err = controller.rpcServers.SyncVoteBits()
		if err != nil {
			log.Error(err)
			return err
		}
	}

	return nil
}

// RPCStart starts the connected rpcServers.
func (controller *MainController) RPCStart() {
	controller.rpcServers.Start()
}

// RPCStop stops the connected rpcServers.
func (controller *MainController) RPCStop() error {
	return controller.rpcServers.Stop()
}

// RPCIsStopped checks to see if w.shutdown is set or not.
func (controller *MainController) RPCIsStopped() bool {
	return controller.rpcServers.IsStopped()
}

// WalletStatus returns current WalletInfo from all rpcServers.
func (controller *MainController) WalletStatus() ([]*dcrjson.WalletInfoResult, error) {
	return controller.rpcServers.WalletStatus()
}

// handlePotentialFatalError is a helper funtion to do log possibly
// fatal rpc errors and also stops the servers to avoid any potential
// further damage.
func (controller *MainController) handlePotentialFatalError(fn string, err error) {
	cnErr, ok := err.(connectionError)
	if ok {
		log.Infof("RPC %s failed on connection error: %v", fn, cnErr)
	}
	controller.RPCStop()
	log.Infof("RPC %s failed: %v", fn, err)
}

// Address renders the address page.
func (controller *MainController) Address(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	c.Env["IsAddress"] = true
	c.Env["Network"] = controller.params.Name

	c.Env["Flash"] = session.Flashes("address")
	widgets := controller.Parse(t, "address", c.Env)

	c.Env["Title"] = "Decred Stake Pool - Address"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// AddressPost is address form submit route.
func (controller *MainController) AddressPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)

	// User may have a session so error out here as well.
	if controller.closePool {
		session.AddFlash(controller.closePoolMsg, "address")
		return controller.Address(c, r)
	}

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	// Only accept address if user does not already have a PubKeyAddr set.
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

	// Get dcrutil.Address for user from pubkey address string
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

	// Get new address from pool wallets
	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	pooladdress, err := controller.rpcServers.GetNewAddress()
	if err != nil {
		controller.handlePotentialFatalError("GetNewAddress", err)
		return "/error", http.StatusSeeOther
	}

	// From new address (pkh), get pubkey address
	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	poolValidateAddress, err := controller.rpcServers.ValidateAddress(pooladdress)
	if err != nil {
		controller.handlePotentialFatalError("ValidateAddress pooladdress", err)
		return "/error", http.StatusSeeOther
	}
	poolPubKeyAddr := poolValidateAddress.PubKeyAddr

	// Get back Address from pool's new pubkey address
	p, err := dcrutil.DecodeAddress(poolPubKeyAddr, controller.params)
	if err != nil {
		controller.handlePotentialFatalError("DecodeAddress poolPubKeyAddr", err)
		return "/error", http.StatusSeeOther
	}

	// Create the the multisig script. Result includes a P2SH and RedeemScript.
	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	createMultiSig, err := controller.rpcServers.CreateMultisig(1, []dcrutil.Address{p, u})
	if err != nil {
		controller.handlePotentialFatalError("CreateMultisig", err)
		return "/error", http.StatusSeeOther
	}

	// Serialize the RedeemScript (hex string -> []byte)
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

	// Import the RedeemScript
	err = controller.rpcServers.ImportScript(serializedScript, int(bestBlockHeight))
	if err != nil {
		controller.handlePotentialFatalError("ImportScript", err)
		return "/error", http.StatusSeeOther
	}

	// Get the pool fees address for this user
	uid64 := session.Values["UserId"].(int64)
	userFeeAddr, err := controller.FeeAddressForUserID(int(uid64))
	if err != nil {
		log.Warnf("unexpected error deriving pool addr: %s", err.Error())
		return "/error", http.StatusSeeOther
	}

	// Update the user's DB entry with multisig, user and pool pubkey
	// addresses, and the fee address
	models.UpdateUserByID(dbMap, uid64, createMultiSig.Address,
		createMultiSig.RedeemScript, poolPubKeyAddr, userPubKeyAddr,
		userFeeAddr.EncodeAddress(), bestBlockHeight)

	return "/tickets", http.StatusSeeOther
}

// EmailUpdate validates the passed token and updates the user's email address.
func (controller *MainController) EmailUpdate(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	// validate that the token is set, valid, and not expired.
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

// EmailVerify renders the email verification page.
func (controller *MainController) EmailVerify(c web.C, r *http.Request) (string,
	int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	// validate that the token is set and valid.
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

// Error renders the error page.
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

	widgets := controller.Parse(t, "error", c.Env)
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Index renders the home page.
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
	//t := c.Env["Template"].(*template.Template)

	// execute the named template with data in c.Env
	widgets := helpers.Parse(t, "home", c.Env)

	// With that kind of flags template can "figure out" what route is being rendered
	c.Env["IsIndex"] = true

	c.Env["Title"] = "Decred Stake Pool - Welcome"
	c.Env["Content"] = template.HTML(widgets)

	return helpers.Parse(t, "main", c.Env), http.StatusOK
}

// PasswordReset renders the password reset page.
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

// PasswordResetPost handles the posted password reset form.
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

// PasswordUpdate renders the password update page.
func (controller *MainController) PasswordUpdate(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	// validate that the token is set, valid, and not expired.
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

// PasswordUpdatePost handles updating passwords.
func (controller *MainController) PasswordUpdatePost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	// validate that the token is set and not expired.
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

// Settings renders the settings page.
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

// SettingsPost handles changing the user's email address or password.
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
			return controller.Settings(c, r)
		}

		// send a confirmation email.
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

// SignIn renders the signin page.
func (controller *MainController) SignIn(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)

	// Tell main.html what route is being rendered
	c.Env["IsSignIn"] = true

	c.Env["Flash"] = session.Flashes("auth")
	widgets := controller.Parse(t, "auth/signin", c.Env)

	c.Env["Title"] = "Decred Stake Pool - Sign In"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// SignInPost is the form submit route. Logs user in or sets an appropriate message in
// session if login was not successful.
func (controller *MainController) SignInPost(c web.C, r *http.Request) (string, int) {
	email, password := r.FormValue("email"), r.FormValue("password")

	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	// Validate email and password combination.
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

	// If pool is closed and user has not yet provided a pubkey address, do not
	// allow login.
	if controller.closePool {
		if len(user.UserPubKeyAddr) == 0 {
			session.AddFlash(controller.closePoolMsg, "auth")
			c.Env["IsClosed"] = true
			c.Env["ClosePoolMsg"] = controller.closePoolMsg
			return controller.SignIn(c, r)
		}
	}

	session.Values["UserId"] = user.Id

	// Go to Address page if multisig script not yet set up.
	if user.MultiSigAddress == "" {
		return "/address", http.StatusSeeOther
	}

	// Go to Tickets page if user already set up.
	return "/tickets", http.StatusSeeOther
}

// SignUp renders the signup page.
func (controller *MainController) SignUp(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)

	// Tell main.html what route is being rendered
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

	widgets := controller.Parse(t, "auth/signup", c.Env)

	c.Env["Title"] = "Decred Stake Pool - Sign Up"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// SignUpPost form submit route. Registers new user or shows Sign Up route with appropriate
// messages set in session.
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

	if !strings.Contains(email, "@") {
		session.AddFlash("email address is invalid", "signupError")
		return controller.SignUp(c, r)
	}

	if password == "" {
		session.AddFlash("password cannot be empty", "signupError")
		return controller.SignUp(c, r)
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

	body := signupEmailTemplate
	body = strings.Replace(body, "__URL__", controller.baseURL, -1)
	body = strings.Replace(body, "__REMOTEIP__", remoteIP, -1)
	body = strings.Replace(body, "__TOKEN__", token, -1)

	err := controller.SendMail(user.Email, signupEmailSubject, body)
	if err != nil {
		session.AddFlash("Unable to send signup email", "signupError")
		log.Errorf("error sending verification email %v", err)
	} else {
		session.AddFlash("A verification email has been sent to "+email, "signupSuccess")
	}

	return controller.SignUp(c, r)
}

// Stats renders the stats page.
func (controller *MainController) Stats(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	c.Env["IsStats"] = true
	c.Env["Title"] = "Decred Stake Pool - Stats"

	dbMap := controller.GetDbMap(c)

	userCount := models.GetUserCount(dbMap)
	userCountActive := models.GetUserCountActive(dbMap)

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
	c.Env["UserCount"] = userCount
	c.Env["UserCountActive"] = userCountActive

	widgets := controller.Parse(t, "stats", c.Env)
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Status renders the status page.
func (controller *MainController) Status(c web.C, r *http.Request) (string, int) {

	// Confirm that the incoming IP address is an approved
	// admin IP as set in config.
	remoteIP := r.RemoteAddr
	if strings.Contains(remoteIP, ":") {
		parts := strings.Split(remoteIP, ":")
		remoteIP = parts[0]
	}

	if !stringSliceContains(controller.adminIPs, remoteIP) {
		return "/error", http.StatusSeeOther
	}

	// Attempt to query wallet statuses
	walletInfo, err := controller.WalletStatus()
	if err != nil {
		// decide when to throw err here
	}

	type WalletInfoPage struct {
		Connected         bool
		DaemonConnected   bool
		Unlocked          bool
		TicketMaxPrice    float64
		BalanceToMaintain float64
		StakeMining       bool
	}
	walletPageInfo := make([]WalletInfoPage, len(walletInfo), len(walletInfo))
	connectedWallets := 0
	for i, v := range walletInfo {
		// If something is nil in the slice means it is disconnected.
		if v == nil {
			walletPageInfo[i] = WalletInfoPage{
				Connected: false,
			}
			controller.rpcServers.DisconnectWalletRPC(i)
			err = controller.rpcServers.ReconnectWalletRPC(i)
			if err != nil {
				log.Infof("wallet rpc reconnect failed: server %v %v", i, err)
			}
			continue
		}
		// Wallet has been successfully queried.
		connectedWallets++
		walletPageInfo[i] = WalletInfoPage{
			Connected:         true,
			DaemonConnected:   v.DaemonConnected,
			Unlocked:          v.Unlocked,
			TicketMaxPrice:    v.TicketMaxPrice,
			BalanceToMaintain: v.BalanceToMaintain,
			StakeMining:       v.StakeMining,
		}
	}

	// Depending on how many wallets have been detected update RPCStatus.
	// Admins can then use to monitor this page periodically and check status.
	rpcstatus := "Unknown"
	allWallets := len(walletInfo)

	if connectedWallets == allWallets {
		rpcstatus = "OK"
	} else {
		if connectedWallets == 0 {
			rpcstatus = "Emergency"
		} else if connectedWallets == 1 {
			rpcstatus = "Critical"
		} else {
			rpcstatus = "Degraded"
		}
	}

	t := controller.GetTemplate(c)
	c.Env["IsStatus"] = true
	c.Env["Title"] = "Decred Stake Pool - Status"

	// Set info to be used by admins on /status page.
	c.Env["WalletInfo"] = walletPageInfo
	c.Env["RPCStatus"] = rpcstatus

	widgets := controller.Parse(t, "status", c.Env)
	c.Env["Content"] = template.HTML(widgets)

	if controller.RPCIsStopped() {
		return controller.Parse(t, "main", c.Env), http.StatusInternalServerError
	}

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Tickets renders the tickets page.
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

	responseHeaderMap := make(map[string]string)
	c.Env["ResponseHeaderMap"] = responseHeaderMap
	// map is a reference type so responseHeaderMap may be modified

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

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}

	// Get P2SH Address
	multisig, err := dcrutil.DecodeAddress(user.MultiSigAddress, controller.params)
	if err != nil {
		c.Env["Error"] = "Invalid multisig data in database"
		log.Infof("Invalid address %v in database: %v", user.MultiSigAddress, err)
	}

	w := controller.rpcServers
	// TODO: Tell the user if there is a cool-down

	// Attempt a "TryLock" so the page won't block

	// select {
	// case <-w.ticketTryLock:
	// 	w.ticketTryLock <- nil
	// 	responseHeaderMap["Retry-After"] = "60"
	// 	c.Env["Content"] = template.HTML("Ticket data resyncing.  Please try again later.")
	// 	return controller.Parse(t, "main", c.Env), http.StatusProcessing
	// default:
	// }

	if atomic.LoadInt32(&w.ticketDataBlocker) != 0 {
		// with HTTP 102 we can specify an estimated time
		responseHeaderMap["Retry-After"] = "60"
		// Render page with messgae to try again later
		//c.Env["Content"] = template.HTML("Ticket data resyncing.  Please try again later.")
		session.AddFlash("Ticket data now resyncing. Please try again later.", "tickets-warning")
		c.Env["FlashWarn"] = session.Flashes("tickets-warning")
		c.Env["Content"] = template.HTML(controller.Parse(t, "tickets", c.Env))
		return controller.Parse(t, "main", c.Env), http.StatusOK
	}

	// Vote bits sync is not running, but we also don't want a sync process
	// starting. Note that the sync process locks this mutex before setting the
	// atomic, so this shouldn't block.
	w.ticketDataLock.RLock()
	defer w.ticketDataLock.RUnlock()

	widgets := controller.Parse(t, "tickets", c.Env)

	// TODO: how could this happen?
	if err != nil {
		log.Info(err)
		widgets = controller.Parse(t, "tickets", c.Env)
		c.Env["Content"] = template.HTML(widgets)
		return controller.Parse(t, "main", c.Env), http.StatusOK
	}

	// spui := new(dcrjson.StakePoolUserInfoResult)
	spui, err := w.StakePoolUserInfo(multisig)
	if err != nil {
		// Render page with messgae to try again later
		log.Infof("RPC StakePoolUserInfo failed: %v", err)
		session.AddFlash("Unable to retreive stake pool user info.", "main")
		c.Env["Flash"] = session.Flashes("main")
		return controller.Parse(t, "main", c.Env), http.StatusInternalServerError
	}

	// If the user has tickets, get their info
	if spui != nil && len(spui.Tickets) > 0 {
		// Only get or set votebits for live tickets
		liveTicketHashes, err := w.GetUnspentUserTickets(multisig)
		if err != nil {
			return "/error?r=/tickets", http.StatusSeeOther
		}

		gtvb, err := w.GetTicketsVoteBits(liveTicketHashes)
		if err != nil {
			if err.Error() == "non equivalent votebits returned" {
				// Launch a goroutine to repair these tickets vote bits
				go w.SyncTicketsVoteBits(liveTicketHashes)
				responseHeaderMap["Retry-After"] = "60"
				// Render page with messgae to try again later
				session.AddFlash("Detected mismatching vote bits.  "+
					"Ticket data is now resyncing.  Please try again after a "+
					"few minutes.", "tickets")
				c.Env["Flash"] = session.Flashes("tickets")
				c.Env["Content"] = template.HTML(controller.Parse(t, "tickets", c.Env))
				// Return with a 503 error indicating when to retry
				return controller.Parse(t, "main", c.Env), http.StatusServiceUnavailable
			}

			log.Infof("GetTicketsVoteBits failed %v", err)
			return "/error?r=/tickets", http.StatusSeeOther
		}

		voteBitMap := make(map[string]uint16)
		for i := range liveTicketHashes {
			voteBitMap[liveTicketHashes[i].String()] = gtvb.VoteBitsList[i].VoteBits
		}

		for idx, ticket := range spui.Tickets {
			switch ticket.Status {
			case "live":
				ticketInfoLive[idx] = TicketInfoLive{
					Ticket:       ticket.Ticket,
					TicketHeight: ticket.TicketHeight,
					VoteBits:     voteBitMap[ticket.Ticket], //gtvbAll.VoteBitsList[idx].VoteBits,
				}
			case "missed":
				ticketInfoMissed[idx] = TicketInfoHistoric{
					Ticket:        ticket.Ticket,
					SpentByHeight: ticket.SpentByHeight,
					TicketHeight:  ticket.TicketHeight,
				}
			case "voted":
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
	c.Env["Flash"] = session.Flashes("tickets")

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// TicketsPost form submit route.
func (controller *MainController) TicketsPost(c web.C, r *http.Request) (string, int) {
	w := controller.rpcServers

	// If already processing let /tickets handle this
	if atomic.LoadInt32(&w.ticketDataBlocker) != 0 {
		return "/tickets", http.StatusSeeOther
	}

	chooseallow := r.FormValue("chooseallow")
	var voteBits = uint16(0)

	if chooseallow == "2" {
		// pool policy and approve.
		// TODO: set policy somewhere else and make it available to /tickets page.
		voteBits = uint16(1)
		voteBits |= approveBlockMask
	} else {
		if chooseallow == "1" {
			voteBits = approveBlockMask
		} else {
			voteBits = disapproveBlockMask
		}
	}

	// Look up user, and try very hard to avoid a panic
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)
	id, ok := session.Values["UserId"].(int64)
	if !ok {
		log.Error("No valid UserID")
	}

	user := models.GetUserById(dbMap, id)
	if user == nil {
		log.Error("Unable to find user with ID", id)
	}

	if user.MultiSigAddress == "" {
		log.Info("Multisigaddress empty")
		return "/error?r=/tickets", http.StatusSeeOther
	}

	multisig, err := dcrutil.DecodeAddress(user.MultiSigAddress, controller.params)
	if err != nil {
		log.Infof("Invalid address %v in database: %v", user.MultiSigAddress, err)
		return "/error?r=/tickets", http.StatusSeeOther
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}

	outPath := "/tickets"
	status := http.StatusSeeOther

	// Set this off in a goroutine
	// TODO: error on channel
	go func() {
		// write lock
		w.ticketDataLock.Lock()
		defer w.ticketDataLock.Unlock()

		var err error
		defer func() { w.setVoteBitsResyncChan <- err }()

		if !atomic.CompareAndSwapInt32(&w.ticketDataBlocker, 0, 1) {
			return
		}
		defer atomic.StoreInt32(&w.ticketDataBlocker, 0)

		// Only get or set votebits for live tickets
		liveTicketHashes, err := w.GetUnspentUserTickets(multisig)
		if err != nil {
			return
		}

		log.Infof("Started setting of vote bits for %d tickets.",
			len(liveTicketHashes))

		vbs := make([]stake.VoteBits, len(liveTicketHashes))
		for i := 0; i < len(liveTicketHashes); i++ {
			vbs[i] = stake.VoteBits{Bits: voteBits}
			//vbs[i].Bits = voteBits
		}

		err = controller.rpcServers.SetTicketsVoteBits(liveTicketHashes, vbs)
		if err != nil {
			if err == ErrSetVoteBitsCoolDown {
				return
			}
			controller.handlePotentialFatalError("SetTicketVoteBits", err)
			return
		}

		log.Infof("Completed setting of vote bits for %d tickets.",
			len(liveTicketHashes))

		return
	}()

	// Like a timeout, give the sync some time to process, otherwise /tickets
	// will show a message that it is still syncing.
	time.Sleep(3 * time.Second)

	return outPath, status
}

// Logout the user.
func (controller *MainController) Logout(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)

	session.Values["UserId"] = nil

	return "/", http.StatusSeeOther
}

func stringSliceContains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
