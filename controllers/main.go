package controllers

import (
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/golang/glog"

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
)

// disapproveBlockMask
const disapproveBlockMask = 0x0000

// approveBlockMask
const approveBlockMask = 0x0001

// MainController
type MainController struct {
	system.Controller

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
}

// NewMainController
func NewMainController(params *chaincfg.Params, closePool bool,
	closePoolMsg string, extPubStr string, poolEmail string, poolFees float64,
	poolLink string, recaptchaSecret string, recaptchaSiteKey string,
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
	}

	return mc, nil
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
	serializedScript, err := hex.DecodeString(createMultiSig.RedeemScript)
	if err != nil {
		controller.handlePotentialFatalError("CreateMultisig DecodeString", err)
		return "/error", http.StatusSeeOther
	}
	err = controller.rpcServers.ImportScript(serializedScript)
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
		userFeeAddr.EncodeAddress())

	return "/tickets", http.StatusSeeOther
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

	// With that kind of flags template can "figure out" what route is being rendered
	c.Env["IsIndex"] = true

	c.Env["Title"] = "Decred Stake Pool - Welcome"
	c.Env["Content"] = template.HTML(widgets)

	return helpers.Parse(t, "main", c.Env), http.StatusOK
}

// Sign in route
func (controller *MainController) SignIn(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)

	// With that kind of flags template can "figure out" what route is being rendered
	c.Env["IsSignIn"] = true

	c.Env["Flash"] = session.Flashes("auth")
	var widgets = controller.Parse(t, "auth/signin", c.Env)

	c.Env["Title"] = "Decred Stake Pool - Sign In"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Sign In form submit route. Logs user in or set appropriate message in session if login was not succesful
func (controller *MainController) SignInPost(c web.C, r *http.Request) (string, int) {
	email, password := r.FormValue("email"), r.FormValue("password")

	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	user, err := helpers.Login(dbMap, email, password)

	if err != nil {
		session.AddFlash("Invalid Email or Password", "auth")
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

	// With that kind of flags template can "figure out" what route is being rendered
	c.Env["IsSignUp"] = true
	if controller.closePool {
		c.Env["IsClosed"] = true
		c.Env["ClosePoolMsg"] = controller.closePoolMsg
	}

	c.Env["Flash"] = session.Flashes("auth")
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

	email, password := r.FormValue("email"), r.FormValue("password")

	session := controller.GetSession(c)

	isValid := re.Verify(*r)
	if !isValid {
		session.AddFlash("Recaptcha error", "auth")
		glog.Errorf("Error whilst registering user: %v", re.LastError())
		return controller.SignUp(c, r)
	}

	dbMap := controller.GetDbMap(c)
	user := models.GetUserByEmail(dbMap, email)

	if user != nil {
		session.AddFlash("User exists", "auth")
		return controller.SignUp(c, r)
	}

	user = &models.User{
		Username: email,
		Email:    email,
	}
	user.HashPassword(password)

	if err := models.InsertUser(dbMap, user); err != nil {
		session.AddFlash("Database error occurred while adding user", "auth")
		glog.Errorf("Error while registering user: %v", err)
		return controller.SignUp(c, r)
	}

	session.Values["UserId"] = user.Id

	return "/address", http.StatusSeeOther
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
	chooseallow, poolcontrol := r.FormValue("chooseallow"), r.FormValue("poolcontrol")
	// votebitsmanual := r.FormValue("votebitsmanual")
	var voteBits = uint16(0)

	if poolcontrol == "1" {
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
