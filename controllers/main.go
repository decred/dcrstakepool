package controllers

import (
	"encoding/hex"
	"net/http"
	"sort"

	"github.com/golang/glog"

	"html/template"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrutil"

	"github.com/decred/dcrstakepool/helpers"
	"github.com/decred/dcrstakepool/models"
	"github.com/decred/dcrstakepool/system"
	"github.com/haisum/recaptcha"
	"github.com/zenazn/goji/web"
)

var DisableSubmissions = true

const disapproveBlockMask = 0x0000
const approveBlockMask = 0x0001

// MainController
type MainController struct {
	system.Controller

	params     *chaincfg.Params
	rpcServers *walletSvrManager
}

// NewMainController
func NewMainController(params *chaincfg.Params) (*MainController, error) {
	rpcs, err := newWalletSvrManager()
	if err != nil {
		return nil, err
	}

	mc := &MainController{
		params:     params,
		rpcServers: rpcs,
	}

	return mc, nil
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
	if DisableSubmissions && controller.params.Name == "mainnet" {
		session.AddFlash("Stake pool is currently oversubscribed", "address")
		return controller.Address(c, r)
	}

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	dbMap := controller.GetDbMap(c)
	user := models.GetUserById(dbMap, session.Values["UserId"].(int64))
	if len(user.Userpubkeyaddr) > 0 {
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

	models.UpdateUserById(dbMap, session.Values["UserId"].(int64), createMultiSig.Address, createMultiSig.RedeemScript, poolPubKeyAddr, userPubKeyAddr)

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

	if DisableSubmissions && controller.params.Name == "mainnet" {
		if len(user.Userpubkeyaddr) == 0 {
			session.AddFlash("Stake pool is currently oversubscribed", "auth")
			c.Env["IsDisabled"] = true
			return controller.SignIn(c, r)
		}
	}

	session.Values["UserId"] = user.Id

	if user.Multisigaddress == "" {
		return "/address", http.StatusSeeOther
	} else {
		return "/tickets", http.StatusSeeOther
	}
}

// Sign up route
func (controller *MainController) SignUp(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)

	// With that kind of flags template can "figure out" what route is being rendered
	c.Env["IsSignUp"] = true
	if DisableSubmissions && controller.params.Name == "mainnet" {
		c.Env["IsDisabled"] = true
	}

	c.Env["Flash"] = session.Flashes("auth")

	var widgets = controller.Parse(t, "auth/signup", c.Env)

	c.Env["Title"] = "Decred Stake Pool - Sign Up"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Sign Up form submit route. Registers new user or shows Sign Up route with appropriate messages set in session
func (controller *MainController) SignUpPost(c web.C, r *http.Request) (string, int) {
	if DisableSubmissions && controller.params.Name == "mainnet" {
		log.Infof("attempt to signup while registration disabled")
		return "/error?r=/signup", http.StatusSeeOther
	}

	re := recaptcha.R{
		Secret: "6LeIxAcTAAAAAGG-vFI1TnRWxMZNFuojJ4WifJWe",
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
	} else {
		return controller.Parse(t, "main", c.Env), http.StatusOK
	}
}

// Tickets page route
func (controller *MainController) Tickets(c web.C, r *http.Request) (string, int) {
	type TicketInfo struct {
		Ticket   string
		VoteBits uint16
	}
	ticketinfo := map[int]TicketInfo{}

	t := controller.GetTemplate(c)
	session := controller.GetSession(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	c.Env["IsTickets"] = true
	c.Env["Network"] = controller.params.Name
	c.Env["Title"] = "Decred Stake Pool - Tickets"

	dbMap := controller.GetDbMap(c)
	user := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	if user.Multisigaddress == "" {
		c.Env["Error"] = "No multisig data has been generated"
		log.Info("Multisigaddress empty")
	}

	ms, err := dcrutil.DecodeAddress(user.Multisigaddress, controller.params)
	if err != nil {
		c.Env["Error"] = "Invalid multisig data in database"
		log.Infof("Invalid address %v in database: %v", user.Multisigaddress, err)
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
	tix, err := controller.rpcServers.TicketsForAddress(ms)
	if err != nil {
		log.Infof("RPC TicketsForAddress failed: %v", err)
		return "/error?r=/tickets", http.StatusSeeOther
	}

	if len(tix.Tickets) > 0 {
		var tickethashes []*chainhash.Hash

		sort.Strings(tix.Tickets)

		for _, ticket := range tix.Tickets {
			th, err := chainhash.NewHashFromStr(ticket)
			if err != nil {
				log.Infof("NewHashFromStr failed for %v", ticket)
				return "/error?r=/tickets", http.StatusSeeOther
			}
			tickethashes = append(tickethashes, th)
		}

		gtvb, err := controller.rpcServers.GetTicketsVoteBits(tickethashes)
		if err != nil {
			log.Infof("GetTicketsVoteBits failed %v", err)
			return "/error?r=/tickets", http.StatusSeeOther
		}

		for idx, ticket := range tix.Tickets {
			ticketinfo[idx] = TicketInfo{ticket, gtvb.VoteBitsList[idx].VoteBits}
		}
	}

	c.Env["Tickets"] = ticketinfo
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

	if user.Multisigaddress == "" {
		log.Info("Multisigaddress empty")
		return "/error?r=/tickets", http.StatusSeeOther
	}

	ms, err := dcrutil.DecodeAddress(user.Multisigaddress, controller.params)
	if err != nil {
		log.Infof("Invalid address %v in database: %v", user.Multisigaddress, err)
		return "/error?r=/tickets", http.StatusSeeOther
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	tix, err := controller.rpcServers.TicketsForAddress(ms)
	if err != nil {
		log.Infof("RPC TicketsForAddress failed: %v", err)
		return "/error?r=/tickets", http.StatusSeeOther
	}

	for _, ticket := range tix.Tickets {
		if controller.RPCIsStopped() {
			return "/error", http.StatusSeeOther
		}
		th, err := chainhash.NewHashFromStr(ticket)
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
