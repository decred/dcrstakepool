// Copyright (c) 2016-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package controllers

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dchest/captcha"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	dcrdatatypes "github.com/decred/dcrdata/api/types/v4"
	"github.com/decred/dcrstakepool/email"
	"github.com/decred/dcrstakepool/helpers"
	"github.com/decred/dcrstakepool/internal/version"
	"github.com/decred/dcrstakepool/models"
	"github.com/decred/dcrstakepool/poolapi"
	"github.com/decred/dcrstakepool/stakepooldclient"
	"github.com/decred/dcrstakepool/system"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/go-gorp/gorp"
	"github.com/gorilla/csrf"
	"github.com/zenazn/goji/web"

	"google.golang.org/grpc/codes"
)

const (
	// MaxUsers is the maximum number of users supported by a voting service.
	// This is an artificial limit and can be increased by adjusting the
	// ticket/fee address indexes above 10000.
	// TODO Remove this limitation by deriving fee addresses from an imported xpub.
	MaxUsers = 10000
	// agendasCacheLife is the amount of time to keep agenda data in memory.
	agendasCacheLife = time.Hour
)

type Config struct {
	AdminIPs             []string
	AdminUserIDs         []string
	APISecret            string
	BaseURL              string
	ClosePool            bool
	ClosePoolMsg         string
	PoolEmail            string
	PoolFees             float64
	PoolLink             string
	RealIPHeader         string
	MaxVotedTickets      int
	Description          string
	Designation          string
	APIVersionsSupported []int
	FeeXpub              *hdkeychain.ExtendedKey
	StakepooldServers    *stakepooldclient.StakepooldManager
	EmailSender          email.Sender
	VotingXpub           *hdkeychain.ExtendedKey

	NetParams *chaincfg.Params
}

// MainController is the wallet RPC controller type.  Its methods include the
// route handlers.
type MainController struct {
	// embed type for c.Env[""] context and ExecuteTemplate helpers
	system.Controller

	Cfg            *Config
	captchaHandler *CaptchaHandler
	voteVersion    uint32
	DCRDataURL     string
}

// agendasCache holds the current available agendas for agendasCacheLife. Should
// be accessed through MainController's agendas method.
var agendasCache agendasMux

// agenda links an agenda to its status. Possible statuses are upcoming,
// in progress, finished, failed, or locked in.
type agenda struct {
	Agenda chaincfg.ConsensusDeployment
	Status string
}

// agendasMux allows for concurrency safe access to agendasCache. Lock must be
// held for read/writes.
type agendasMux struct {
	sync.Mutex
	timer   time.Time
	agendas *[]agenda
}

// Get the client's real IP address using the X-Real-IP header, or if that is
// empty, http.Request.RemoteAddr. See the sample nginx.conf for using the
// real_ip module to correctly set the X-Real-IP header.
func getClientIP(r *http.Request, realIPHeader string) string {
	// getHost returns the host portion of a string containing either a
	// host:port formatted name or just a host.
	getHost := func(hostPort string) string {
		ip, _, err := net.SplitHostPort(hostPort)
		if err != nil {
			return hostPort
		}
		return ip
	}

	// If header not set, return RemoteAddr. Invalid hosts are replaced with "".
	if realIPHeader == "" {
		return getHost(r.RemoteAddr)
	}
	return getHost(r.Header.Get(realIPHeader))
}

// NewMainController is the constructor for the entire controller routing.
func NewMainController(cfg *Config) (*MainController, error) {
	ch := &CaptchaHandler{
		ImgHeight: 127,
		ImgWidth:  257,
	}

	mc := &MainController{
		Cfg:            cfg,
		captchaHandler: ch,
	}

	walletInfo, err := cfg.StakepooldServers.WalletInfo()
	if err != nil {
		cErr := fmt.Errorf("Failed to get wallets' Vote Version: %v", err)
		return nil, cErr
	}

	// Ensure vote version matches on all wallets
	lastVersion := uint32(0)
	var lastServer int
	firstrun := true
	for k, v := range walletInfo {
		if firstrun {
			firstrun = false
			lastVersion = v.VoteVersion
		}

		if v.VoteVersion != lastVersion {
			vErr := fmt.Errorf("wallets %d and %d have mismatched vote versions",
				k, lastServer)
			return nil, vErr
		}

		lastServer = k
	}

	log.Infof("All wallets are VoteVersion %d", lastVersion)

	mc.voteVersion = lastVersion

	mc.DCRDataURL = fmt.Sprintf("https://%s.dcrdata.org", mc.getNetworkName())

	return mc, nil
}

// getNetworkName will strip any suffix from a network name starting with
// "testnet" (e.g. "testnet3"). This is primarily intended for the tickets page,
// which generates block explorer links using a value set by the network string,
// which is a problem since there is no testnet3.dcrdata.org host.
func (controller *MainController) getNetworkName() string {
	if strings.HasPrefix(controller.Cfg.NetParams.Name, "testnet") {
		return "testnet"
	}
	return controller.Cfg.NetParams.Name
}

// agendas returns agendas and their statuses. Fetches agenda status from
// dcrdata.org if past agenda.Timer limit from previous fetch. Caches agenda
// data for agendasCacheLife. This method is safe for concurrent use.
func (controller *MainController) agendas() []agenda {
	agendasCache.Lock()
	defer agendasCache.Unlock()
	now := time.Now()
	if agendasCache.timer.After(now) {
		return *agendasCache.agendas
	}
	agendasCache.timer = now.Add(agendasCacheLife)
	url := fmt.Sprintf("%s/api/agendas", controller.DCRDataURL)
	agendaInfos, err := dcrDataAgendas(url)
	if err != nil {
		// Ensure the next call tries to fetch statuses again.
		agendasCache.timer = time.Time{}
		log.Warnf("unable to retrieve data from %v: %v", url, err)
		// If we have initialized agendas, return that.
		if agendasCache.agendas != nil {
			return *agendasCache.agendas
		}
	}
	agendaArray := controller.getAgendas()
	agendasNew := make([]agenda, len(agendaArray))
	// populate agendas
	for n, agenda := range agendaArray {
		agendasNew[n].Agenda = agenda
		// find status for id
		for _, info := range agendaInfos {
			if info.Name == agenda.Vote.Id {
				agendasNew[n].Status = info.Status.String()
				break
			}
		}
	}
	agendasCache.agendas = &agendasNew
	return *agendasCache.agendas
}

// dcrDataAgendas gets json data for current agendas from url. url is either
// https://testnet.dcrdata.org/api/agendas or https://mainnet.dcrdata.org/api/agendas
func dcrDataAgendas(url string) ([]*dcrdatatypes.AgendasInfo, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	data, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	a := []*dcrdatatypes.AgendasInfo{}
	if err = json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return a, nil
}

// API is the main frontend that handles all API requests.
func (controller *MainController) API(c web.C, r *http.Request) *system.APIResponse {
	command := c.URLParams["command"]

	// poolapi.Response comprises a status, code, message, and a data struct
	var code codes.Code
	var response, status string
	var data interface{}

	var err error

	switch r.Method {
	case "GET":
		switch command {
		case "getpurchaseinfo":
			data, code, response, err = controller.APIPurchaseInfo(c, r)
		case "stats":
			data, code, response, err = controller.APIStats(c, r)
		default:
			return nil
		}
	case "POST":
		switch command {
		case "address":
			_, code, response, err = controller.APIAddress(c, r)
		case "voting":
			_, code, response, err = controller.APIVoting(c, r)
		default:
			return nil
		}
	}

	if err != nil {
		status = "error"
		response = response + " - " + err.Error()
	} else {
		status = "success"
	}

	return system.NewAPIResponse(status, code, response, data)
}

// APIAddress is the API version of AddressPost
func (controller *MainController) APIAddress(c web.C, r *http.Request) ([]string, codes.Code, string, error) {
	dbMap := controller.GetDbMap(c)

	if c.Env["APIUserID"] == nil {
		return nil, codes.Unauthenticated, "address error", errors.New("invalid api token")
	}

	user, _ := models.GetUserById(dbMap, c.Env["APIUserID"].(int64))

	if len(user.UserPubKeyAddr) > 0 {
		return nil, codes.AlreadyExists, "address error", errors.New("address already submitted")
	}

	userPubKeyAddr := r.FormValue("UserPubKeyAddr")

	if _, err := validateUserPubKeyAddr(userPubKeyAddr, controller.Cfg.NetParams); err != nil {
		return nil, codes.InvalidArgument, "address error", err
	}

	// Get the ticket address for this user
	pooladdress, err := controller.TicketAddressForUserID(int(c.Env["APIUserID"].(int64)))
	if err != nil {
		log.Errorf("unable to derive ticket address: %v", err)
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	poolValidateAddress, err := controller.Cfg.StakepooldServers.ValidateAddress(pooladdress)
	if err != nil {
		log.Errorf("unable to validate address: %v", err)
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}
	if !poolValidateAddress.IsMine {
		log.Errorf("unable to validate ismine for pool ticket address: %s",
			pooladdress.String())
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	poolPubKeyAddr := poolValidateAddress.PubKeyAddr

	if _, err = dcrutil.DecodeAddress(poolPubKeyAddr, controller.Cfg.NetParams); err != nil {
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	createMultiSig, err := controller.Cfg.StakepooldServers.CreateMultisig([]string{poolPubKeyAddr, userPubKeyAddr})
	if err != nil {
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	// Serialize the redeem script (hex string -> []byte)
	serializedScript, err := hex.DecodeString(createMultiSig.RedeemScript)
	if err != nil {
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	// Import the redeem script
	var importedHeight int64
	importedHeight, err = controller.Cfg.StakepooldServers.ImportNewScript(serializedScript)
	if err != nil {
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	userFeeAddr, err := controller.FeeAddressForUserID(int(user.Id))
	if err != nil {
		log.Warnf("unexpected error deriving pool addr: %s", err.Error())
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	models.UpdateUserByID(dbMap, user.Id, createMultiSig.Address,
		createMultiSig.RedeemScript, poolPubKeyAddr, userPubKeyAddr,
		userFeeAddr.Address(), importedHeight)

	log.Infof("successfully create multisigaddress for user %d", c.Env["APIUserID"])

	err = controller.StakepooldUpdateUsers(dbMap)
	if err != nil {
		log.Warnf("failure to update users: %v", err)
	}

	return nil, codes.OK, "address successfully imported", nil
}

// APIPurchaseInfo fetches and returns the user's info or an error
func (controller *MainController) APIPurchaseInfo(c web.C,
	r *http.Request) (*poolapi.PurchaseInfo, codes.Code, string, error) {
	dbMap := controller.GetDbMap(c)

	if c.Env["APIUserID"] == nil {
		return nil, codes.Unauthenticated, "purchaseinfo error", errors.New("invalid api token")
	}

	user, _ := models.GetUserById(dbMap, c.Env["APIUserID"].(int64))

	if len(user.UserPubKeyAddr) == 0 {
		return nil, codes.FailedPrecondition, "purchaseinfo error", errors.New("no address submitted")
	}

	purchaseInfo := &poolapi.PurchaseInfo{
		PoolAddress:   user.UserFeeAddr,
		PoolFees:      controller.Cfg.PoolFees,
		Script:        user.MultiSigScript,
		TicketAddress: user.MultiSigAddress,
		VoteBits:      uint16(user.VoteBits),
	}

	return purchaseInfo, codes.OK, "purchaseinfo successfully retrieved", nil
}

// APIStats is an API version of the stats page
func (controller *MainController) APIStats(c web.C,
	r *http.Request) (*poolapi.Stats, codes.Code, string, error) {
	dbMap := controller.GetDbMap(c)
	userCount := models.GetUserCount(dbMap)
	userCountActive := models.GetUserCountActive(dbMap)

	gsi, err := controller.Cfg.StakepooldServers.GetStakeInfo()
	if err != nil {
		log.Infof("RPC GetStakeInfo failed: %v", err)
		return nil, codes.Unavailable, "stats error", errors.New("RPC server error")
	}

	var poolStatus string
	if controller.Cfg.ClosePool {
		poolStatus = "Closed"
	} else {
		poolStatus = "Open"
	}

	stats := &poolapi.Stats{
		AllMempoolTix:        gsi.AllMempoolTix,
		APIVersionsSupported: controller.Cfg.APIVersionsSupported,
		BlockHeight:          gsi.BlockHeight,
		Difficulty:           gsi.Difficulty,
		Expired:              gsi.Expired,
		Immature:             gsi.Immature,
		Live:                 gsi.Live,
		Missed:               gsi.Missed,
		OwnMempoolTix:        gsi.OwnMempoolTix,
		PoolSize:             gsi.PoolSize,
		ProportionLive:       gsi.ProportionLive,
		ProportionMissed:     gsi.ProportionMissed,
		Revoked:              gsi.Revoked,
		TotalSubsidy:         gsi.TotalSubsidy,
		Voted:                gsi.Voted,
		Network:              controller.Cfg.NetParams.Name,
		PoolEmail:            controller.Cfg.PoolEmail,
		PoolFees:             controller.Cfg.PoolFees,
		PoolStatus:           poolStatus,
		UserCount:            userCount,
		UserCountActive:      userCountActive,
		Version:              version.String(),
	}

	return stats, codes.OK, "stats successfully retrieved", nil
}

// APIVoting is the API version of Voting
func (controller *MainController) APIVoting(c web.C, r *http.Request) ([]string, codes.Code, string, error) {
	dbMap := controller.GetDbMap(c)

	if c.Env["APIUserID"] == nil {
		return nil, codes.Unauthenticated, "voting error", errors.New("invalid api token")
	}

	user, _ := models.GetUserById(dbMap, c.Env["APIUserID"].(int64))
	oldVoteBits := user.VoteBits

	vb := r.FormValue("VoteBits")
	vbi, err := strconv.Atoi(vb)
	if err != nil {
		return nil, codes.InvalidArgument, "voting error", errors.New("unable to convert votebits to uint16")
	}
	userVoteBits := uint16(vbi)

	if !controller.IsValidVoteBits(userVoteBits) {
		return nil, codes.InvalidArgument, "voting error", errors.New("votebits invalid for current agendas")
	}

	user, err = helpers.UpdateVoteBitsByID(dbMap, user.Id, userVoteBits)
	if err != nil {
		return nil, codes.Internal, "voting error", errors.New("failed to update voting prefs in database")
	}

	if uint16(oldVoteBits) != userVoteBits {
		if err := controller.StakepooldUpdateUsers(dbMap); err != nil {
			log.Warnf("APIVoting: StakepooldUpdateUsers failed: %v", err)
		}
	}

	log.Infof("updated voteBits for user %d from %d to %d",
		user.Id, oldVoteBits, userVoteBits)

	return nil, codes.OK, "successfully updated voting preferences", nil
}

func (controller *MainController) isAdmin(c web.C, r *http.Request) (bool, error) {
	remoteIP := getClientIP(r, controller.Cfg.RealIPHeader)
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)

	if session.Values["UserId"] == nil {
		return false, fmt.Errorf("%s request with no session from %s",
			r.URL, remoteIP)
	}

	uidstr := strconv.Itoa(int(session.Values["UserId"].(int64)))

	if !stringSliceContains(controller.Cfg.AdminIPs, remoteIP) {
		return false, fmt.Errorf("%s request from %s "+
			"userid %s failed AdminIPs check", r.URL, remoteIP, uidstr)
	}

	if !stringSliceContains(controller.Cfg.AdminUserIDs, uidstr) {
		return false, fmt.Errorf("%s request from %s "+
			"userid %s failed adminUserIDs check", r.URL, remoteIP, uidstr)
	}

	return true, nil
}

// StakepooldUpdateTickets attempts to trigger all connected stakepoold
// instances to pull a data update of the specified kind.
func (controller *MainController) StakepooldUpdateTickets(dbMap *gorp.DbMap) error {
	votableLowFeeTickets, err := models.GetVotableLowFeeTickets(dbMap)
	if err != nil {
		return err
	}

	err = controller.Cfg.StakepooldServers.SetAddedLowFeeTickets(votableLowFeeTickets)
	if err != nil {
		log.Errorf("error updating tickets on stakepoold: %v", err)
		return err
	}

	return nil
}

// StakepooldUpdateUsers attempts to trigger all connected stakepoold
// instances to pull a data update of the specified kind.
func (controller *MainController) StakepooldUpdateUsers(dbMap *gorp.DbMap) error {
	// reset votebits if Vote Version changed or if the stored VoteBits are
	// somehow invalid
	allUsers, err := controller.CheckAndResetUserVoteBits(dbMap)
	if err != nil {
		return err
	}

	err = controller.Cfg.StakepooldServers.SetUserVotingPrefs(allUsers)
	if err != nil {
		log.Errorf("error updating users on stakepoold: %v", err)
		return err
	}

	return nil
}

// FeeAddressForUserID generates a unique payout address per used ID for
// fees for an individual pool user.
func (controller *MainController) FeeAddressForUserID(uid int) (dcrutil.Address,
	error) {
	if uid+1 > MaxUsers {
		return nil, fmt.Errorf("bad uid index %v", uid)
	}

	acctKey := controller.Cfg.FeeXpub
	index := uint32(uid)

	// Derive the appropriate branch key
	branchKey, err := acctKey.Child(udb.ExternalBranch)
	if err != nil {
		return nil, err
	}

	key, err := branchKey.Child(index)
	if err != nil {
		return nil, err
	}

	addr, err := helpers.DCRUtilAddressFromExtendedKey(key, controller.Cfg.NetParams)
	if err != nil {
		return nil, err
	}

	return addr, nil
}

// TicketAddressForUserID generates a unique ticket address per used ID for
// generating the 1-of-2 multisig.
func (controller *MainController) TicketAddressForUserID(uid int) (dcrutil.Address,
	error) {
	if uid+1 > MaxUsers {
		return nil, fmt.Errorf("bad uid index %v", uid)
	}

	acctKey := controller.Cfg.VotingXpub
	index := uint32(uid)

	// Derive the appropriate branch key
	branchKey, err := acctKey.Child(udb.ExternalBranch)
	if err != nil {
		return nil, err
	}

	key, err := branchKey.Child(index)
	if err != nil {
		return nil, err
	}

	addr, err := helpers.DCRUtilAddressFromExtendedKey(key, controller.Cfg.NetParams)
	if err != nil {
		return nil, err
	}

	return addr, nil
}

// RPCSync checks to ensure that the wallets are synced on startup.
func (controller *MainController) RPCSync(dbMap *gorp.DbMap) error {
	multisigScripts, err := models.GetAllCurrentMultiSigScripts(dbMap)
	if err != nil {
		return err
	}

	err = controller.Cfg.StakepooldServers.SyncAll(multisigScripts, MaxUsers)
	return err
}

// CheckAndResetUserVoteBits reset users VoteBits if the VoteVersion has
// changed or if the stored VoteBits are somehow invalid.
func (controller *MainController) CheckAndResetUserVoteBits(dbMap *gorp.DbMap) (map[int64]*models.User, error) {
	defaultVoteBits := uint16(1)
	userMax := models.GetUserMax(dbMap)
	for userid := int64(1); userid <= userMax; userid++ {
		// may have gaps due to users deleted from the database
		user, err := models.GetUserById(dbMap, userid)
		if err != nil {
			continue
		}

		// Reset the user's voting preferences if the Vote Version changed
		// since they no longer apply
		if uint32(user.VoteBitsVersion) != controller.voteVersion {
			oldVoteBitsVersion := user.VoteBitsVersion
			_, err := helpers.UpdateVoteBitsVersionByID(dbMap, userid, controller.voteVersion)
			if err != nil {
				return nil, fmt.Errorf("failed to update VoteBitsVersion for uid %v: %v",
					userid, err)
			}

			log.Infof("updated VoteBitsVersion from %v to %v for uid %v",
				oldVoteBitsVersion, controller.voteVersion, userid)

			if uint16(user.VoteBits) != defaultVoteBits {
				oldVoteBits := user.VoteBits
				_, err = helpers.UpdateVoteBitsByID(dbMap, userid, defaultVoteBits)
				if err != nil {
					return nil, fmt.Errorf("failed to update VoteBits for uid %v: %v",
						userid, err)
				}

				log.Infof("updated VoteBits from %v to %v for uid %v",
					oldVoteBits, defaultVoteBits, userid)
			}
		} else if !controller.IsValidVoteBits(uint16(user.VoteBits)) {
			// Validate that the votebits are valid for the agendas of the current
			// vote version
			oldVoteBits := user.VoteBits
			_, err := helpers.UpdateVoteBitsByID(dbMap, userid, defaultVoteBits)
			if err != nil {
				return nil, fmt.Errorf("failed to reset invalid VoteBits for uid %v: %v",
					userid, err)
			}

			log.Infof("reset invalid VoteBits from %v to %v for uid %v",
				oldVoteBits, defaultVoteBits, userid)
		}
	}

	allUsers := make(map[int64]*models.User)
	for userid := int64(1); userid <= userMax; userid++ {
		// may have gaps due to users deleted from the database
		user, err := models.GetUserById(dbMap, userid)
		if err != nil || user.MultiSigAddress == "" {
			continue
		}

		allUsers[user.Id] = user
	}

	return allUsers, nil
}

// Address renders the address page.
func (controller *MainController) Address(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["IsAddress"] = true
	c.Env["PoolFees"] = controller.Cfg.PoolFees
	c.Env["Network"] = controller.getNetworkName()

	c.Env["Flash"] = session.Flashes("address")
	user, _ := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	// Generate an API Token for the user on demand if one does not exist and
	// refresh the user's data before displaying it.
	if user.APIToken == "" {
		token, err := models.SetUserAPIToken(dbMap, controller.Cfg.APISecret,
			controller.Cfg.BaseURL, user.Id)
		if err != nil {
			session.AddFlash("Unable to set API Token", "settingsError")
			log.Errorf("could not set API Token for UserId %v", user.Id)
		}

		c.Env["APIToken"] = token
	} else {
		c.Env["APIToken"] = user.APIToken
	}

	widgets := controller.Parse(t, "address", c.Env)

	c.Env["Title"] = "Decred VSP - Address"
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

func validateUserPubKeyAddr(pubKeyAddr string, params *chaincfg.Params) (dcrutil.Address, error) {
	if len(pubKeyAddr) < 40 {
		str := "Address is too short"
		log.Warnf("User submitted invalid address: %s - %s", pubKeyAddr, str)
		return nil, errors.New(str)
	}

	if len(pubKeyAddr) > 65 {
		str := "Address is too long"
		log.Warnf("User submitted invalid address: %s - %s", pubKeyAddr, str)
		return nil, errors.New(str)
	}

	u, err := dcrutil.DecodeAddress(pubKeyAddr, params)
	if err != nil {
		log.Warnf("User submitted invalid address: %s - %v", pubKeyAddr, err)
		return nil, errors.New("Couldn't decode address")
	}

	_, is := u.(*dcrutil.AddressSecpPubKey)
	if !is {
		str := "Incorrect address type"
		log.Warnf("User submitted invalid address: %s - %s", pubKeyAddr, str)
		return nil, errors.New(str)
	}

	return u, nil
}

// AddressPost is address form submit route.
func (controller *MainController) AddressPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	remoteIP := getClientIP(r, controller.Cfg.RealIPHeader)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}
	uid64 := session.Values["UserId"].(int64)

	// Only accept address if user does not already have a PubKeyAddr set.
	dbMap := controller.GetDbMap(c)
	user, _ := models.GetUserById(dbMap, session.Values["UserId"].(int64))
	if len(user.UserPubKeyAddr) > 0 {
		session.AddFlash("The voting service is currently limited to one address per account", "address")
		return controller.Address(c, r)
	}

	userPubKeyAddr := r.FormValue("UserPubKeyAddr")

	log.Infof("Address POST from %v, pubkeyaddr %v", remoteIP, userPubKeyAddr)

	if _, err := validateUserPubKeyAddr(userPubKeyAddr, controller.Cfg.NetParams); err != nil {
		session.AddFlash(err.Error(), "address")
		return controller.Address(c, r)
	}

	// Get the ticket address for this user
	pooladdress, err := controller.TicketAddressForUserID(int(uid64))
	if err != nil {
		log.Errorf("unable to derive ticket address: %v", err)
		session.AddFlash("Unable to derive ticket address", "address")
		return controller.Address(c, r)
	}

	// From new address (pkh), get pubkey address
	poolValidateAddress, err := controller.Cfg.StakepooldServers.ValidateAddress(pooladdress)
	if err != nil {
		return "/error", http.StatusSeeOther
	}
	if !poolValidateAddress.IsMine {
		log.Errorf("unable to validate ismine for pool ticket address: %s",
			pooladdress.String())
		session.AddFlash("Unable to validate pool ticket address", "address")
		return controller.Address(c, r)
	}
	poolPubKeyAddr := poolValidateAddress.PubKeyAddr

	// Get back Address from pool's new pubkey address
	if _, err = dcrutil.DecodeAddress(poolPubKeyAddr, controller.Cfg.NetParams); err != nil {
		return "/error", http.StatusSeeOther
	}

	// Create the the multisig script. Result includes a P2SH and redeem script.
	createMultiSig, err := controller.Cfg.StakepooldServers.CreateMultisig([]string{poolPubKeyAddr, userPubKeyAddr})
	if err != nil {
		return "/error", http.StatusSeeOther
	}

	// Serialize the redeem script (hex string -> []byte)
	serializedScript, err := hex.DecodeString(createMultiSig.RedeemScript)
	if err != nil {
		return "/error", http.StatusSeeOther
	}

	// Import the redeem script
	var importedHeight int64
	importedHeight, err = controller.Cfg.StakepooldServers.ImportNewScript(serializedScript)
	if err != nil {
		return "/error", http.StatusSeeOther
	}

	// Get the pool fees address for this user
	userFeeAddr, err := controller.FeeAddressForUserID(int(uid64))
	if err != nil {
		log.Errorf("unexpected error deriving fee addr: %s", err.Error())
		session.AddFlash("Unable to derive fee address", "address")
		return controller.Address(c, r)
	}

	// Update the user's DB entry with multisig, user and pool pubkey
	// addresses, and the fee address
	models.UpdateUserByID(dbMap, uid64, createMultiSig.Address,
		createMultiSig.RedeemScript, poolPubKeyAddr, userPubKeyAddr,
		userFeeAddr.Address(), importedHeight)

	if err = controller.StakepooldUpdateUsers(dbMap); err != nil {
		log.Errorf("unable to update all: %v", err)
	}

	return "/tickets", http.StatusSeeOther
}

// AdminStatus renders the status page.
func (controller *MainController) AdminStatus(c web.C, r *http.Request) (string, int) {
	isAdmin, err := controller.isAdmin(c, r)
	if !isAdmin {
		log.Warnf("isAdmin check failed: %v", err)
		return "", http.StatusUnauthorized
	}

	backendStatus := controller.Cfg.StakepooldServers.BackendStatus()

	t := controller.GetTemplate(c)
	c.Env["Admin"] = isAdmin
	c.Env["IsAdminStatus"] = true
	c.Env["Title"] = "Decred Voting Service - Status (Admin)"

	// Set info to be used by admins on /status page.
	c.Env["BackendStatus"] = backendStatus

	widgets := controller.Parse(t, "admin/status", c.Env)
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// AdminTickets renders the administrative tickets page.
// Tickets purchased with an incorrect VSP fee will be listed on this page.
// Admin users can choose whether the pool should vote these tickets or not.
func (controller *MainController) AdminTickets(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)

	isAdmin, err := controller.isAdmin(c, r)
	if !isAdmin {
		log.Warnf("isAdmin check failed: %v", err)
		return "", http.StatusUnauthorized
	}

	votableLowFeeTickets := make(map[chainhash.Hash]string)
	gvlft, err := models.GetVotableLowFeeTickets(dbMap)
	if err == nil {
		for _, t := range gvlft {
			th, _ := chainhash.NewHashFromStr(t.TicketHash)
			votableLowFeeTickets[*th] = t.TicketAddress
		}
	}

	ignoredLowFeeTickets, err := controller.Cfg.StakepooldServers.GetIgnoredLowFeeTickets()
	if err != nil {
		log.Errorf("Could not retrieve ignored low fee tickets from stakepoold: %v", err)
		session.AddFlash("Could not retrieve ignored low fee tickets from stakepoold", "adminTicketsError")
	}

	c.Env["Admin"] = isAdmin
	c.Env["IsAdminTickets"] = true
	c.Env["DCRDataURL"] = controller.DCRDataURL

	c.Env["FlashError"] = session.Flashes("adminTicketsError")
	c.Env["FlashSuccess"] = session.Flashes("adminTicketsSuccess")

	c.Env["AddedLowFeeTickets"] = votableLowFeeTickets
	c.Env["IgnoredLowFeeTickets"] = ignoredLowFeeTickets

	widgets := controller.Parse(t, "admin/tickets", c.Env)

	c.Env["Title"] = "Decred Voting Service - Tickets (Admin)"
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// AdminTicketsPost validates and processes the form posted from AdminTickets.
func (controller *MainController) AdminTicketsPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)
	remoteIP := getClientIP(r, controller.Cfg.RealIPHeader)

	userID, ok := session.Values["UserId"].(int64)
	if !ok {
		log.Warnf("UserId not set!")
		return "", http.StatusUnauthorized
	}

	isAdmin, err := controller.isAdmin(c, r)
	if !isAdmin {
		log.Warnf("isAdmin check failed: %v", err)
		return "", http.StatusUnauthorized
	}

	if err := r.ParseForm(); err != nil {
		session.AddFlash("unable to parse form: "+err.Error(),
			"adminTicketsError")
		return "/admintickets", http.StatusSeeOther
	}

	ticketList := r.PostForm["tickets[]"]

	if len(ticketList) == 0 {
		session.AddFlash("no tickets selected to modify", "adminTicketsError")
		return "/admintickets", http.StatusSeeOther
	}

	// Validate each of the ticket hash strings.
	ticketHashes, err := models.DecodeHashList(ticketList)
	if err != nil {
		session.AddFlash("Invalid ticket in form data: "+err.Error(),
			"adminTicketsError")
		return "/admintickets", http.StatusSeeOther
	}

	action := strings.ToLower(r.PostFormValue("action"))
	switch action {
	case "add", "remove":
		// recognized action
	default:
		session.AddFlash("invalid or unknown form action type", "adminTicketsError")
		return "/admintickets", http.StatusSeeOther
	}

	actionVerb := "unknown"
	switch action {
	case "add":
		actionVerb = "added"
		ignoredLowFeeTickets, err := controller.Cfg.StakepooldServers.GetIgnoredLowFeeTickets()
		if err != nil {
			session.AddFlash("GetIgnoredLowFeeTickets error: "+err.Error(),
				"adminTicketsError")
			return "/admintickets", http.StatusSeeOther
		}

		for i, tickethash := range ticketHashes {
			t := ticketList[i]

			// TODO check if it is already present in the database
			// and error out if so

			msa, exists := ignoredLowFeeTickets[tickethash]
			if !exists {
				session.AddFlash("ticket " + t + " is no longer present")
				return "/admintickets", http.StatusSeeOther
			}

			expires := controller.CalcEstimatedTicketExpiry()

			lowFeeTicket := &models.LowFeeTicket{
				AddedByUid:    userID,
				TicketAddress: msa,
				TicketHash:    t,
				TicketExpiry:  0,
				Voted:         0,
				Created:       time.Now().Unix(),
				Expires:       expires.Unix(),
			}

			err = models.InsertLowFeeTicket(dbMap, lowFeeTicket)
			if err != nil {
				session.AddFlash("Database error occurred while adding ticket "+
					t, "adminTicketsError")
				log.Warnf("Adding ticket %v failed: %v", tickethash, err)
				return "/admintickets", http.StatusSeeOther
			}
		}

	case "remove":
		actionVerb = "removed"
		// To use gorm's slice expansion, use a mapper with a ticketList. For
		// three tickets in the list, gorm will expand this to:
		//     "... IN (:Tickets0,:Tickets1,:Tickets2)".
		// This allows each string in the list two be it's own argument.
		query := "DELETE FROM LowFeeTicket WHERE TicketHash IN (:Tickets)"
		ticketListMapper := map[string]interface{}{
			"Tickets": ticketList,
		}
		_, err := dbMap.Exec(query, ticketListMapper)
		if err != nil {
			session.AddFlash("failed to execute delete query: "+err.Error(),
				"adminTicketsError")
			return "/admintickets", http.StatusSeeOther
		}
	}

	err = controller.StakepooldUpdateTickets(dbMap)
	if err != nil {
		session.AddFlash("StakepooldUpdateAll error: "+err.Error(), "adminTicketsError")
	}

	log.Infof("ip %s userid %d %s for %d ticket(s)", remoteIP, userID,
		actionVerb, len(ticketList))
	session.AddFlash(fmt.Sprintf("Successfully %s %d ticket(s)", actionVerb,
		len(ticketList)), "adminTicketsSuccess")

	return "/admintickets", http.StatusSeeOther
}

// EmailUpdate validates the passed token and updates the user's email address.
func (controller *MainController) EmailUpdate(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)

	render := func() string {
		c.Env["Title"] = "Decred Voting Service - Email Update"
		c.Env["FlashError"] = session.Flashes("emailupdateError")
		c.Env["FlashSuccess"] = session.Flashes("emailupdateSuccess")
		c.Env["IsEmailUpdate"] = true
		widgets := controller.Parse(t, "emailupdate", c.Env)
		c.Env["Designation"] = controller.Cfg.Designation

		c.Env["Content"] = template.HTML(widgets)
		return controller.Parse(t, "main", c.Env)
	}

	// Validate that the token is set.
	tokenStr := r.URL.Query().Get("t")
	if tokenStr == "" {
		session.AddFlash("No email verification token present",
			"emailupdateError")
		return render(), http.StatusOK
	}

	// Validate that the token is valid.
	token, err := models.UserTokenFromStr(tokenStr)
	if err != nil {
		session.AddFlash("Email verification token not valid.",
			"emailupdateError")
		return render(), http.StatusOK
	}

	// Validate that the token is recognized.
	emailChange, err := helpers.EmailChangeTokenExists(dbMap, token)
	if err != nil {
		session.AddFlash("Email verification token not recognized.",
			"emailupdateError")
		return render(), http.StatusOK
	}

	// Validate that the token is not expired.
	expTime := time.Unix(emailChange.Expires, 0)
	if expTime.Before(time.Now()) {
		session.AddFlash("Email change token has expired.",
			"emailupdateError")
		return render(), http.StatusOK
	}

	// possible that someone signed up with this email in the time between
	// when the token was generated and now.
	userExists := models.GetUserByEmail(dbMap, emailChange.NewEmail)
	if userExists != nil {
		session.AddFlash("Email address is in use", "emailupdateError")
		return render(), http.StatusOK
	}

	err = helpers.EmailChangeComplete(dbMap, token)
	if err != nil {
		session.AddFlash("Error occurred while changing email address",
			"emailupdateError")
		log.Errorf("EmailUpdate: EmailChangeComplete failed %v", err)
	} else {
		// destroy session data and force re-login
		userID, _ := session.Values["UserId"].(int64)
		session.Options.MaxAge = -1
		if err := system.DestroySessionsForUserID(dbMap, userID); err != nil {
			log.Warnf("EmailUpdate: DestroySessionsForUserID '%v' failed: %v",
				userID, err)
		}

		session.AddFlash("Email successfully updated",
			"emailupdateSuccess")
	}
	return render(), http.StatusOK
}

// EmailVerify renders the email verification page.
func (controller *MainController) EmailVerify(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)

	render := func() string {
		c.Env["Title"] = "Decred Voting Service - Email Verification"
		c.Env["FlashError"] = session.Flashes("emailverifyError")
		c.Env["FlashSuccess"] = session.Flashes("emailverifySuccess")
		c.Env["IsEmailVerify"] = true
		widgets := controller.Parse(t, "emailverify", c.Env)
		c.Env["Designation"] = controller.Cfg.Designation

		c.Env["Content"] = template.HTML(widgets)
		return controller.Parse(t, "main", c.Env)
	}

	// Validate that the token is set.
	tokenStr := r.URL.Query().Get("t")
	if tokenStr == "" {
		session.AddFlash("No email verification token present.",
			"emailverifyError")
		return render(), http.StatusOK
	}

	// Validate that the token is valid.
	token, err := models.UserTokenFromStr(tokenStr)
	if err != nil {
		session.AddFlash("Email verification token not valid.",
			"emailverifyError")
		return render(), http.StatusOK
	}

	// Validate that the token is recognized.
	_, err = helpers.EmailVerificationTokenExists(dbMap, token)
	if err != nil {
		session.AddFlash("Email verification token not recognized.",
			"emailverifyError")
		return render(), http.StatusOK
	}

	// Set the email as verified.
	err = helpers.EmailVerificationComplete(dbMap, token)
	if err != nil {
		session.AddFlash("Unable to set email to verified status.",
			"emailverifyError")
		log.Errorf("could not set email to verified %v", err)
		return render(), http.StatusInternalServerError
	}

	session.AddFlash("Email successfully verified.",
		"emailverifySuccess")
	return render(), http.StatusOK
}

// Error renders the error page.
func (controller *MainController) Error(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)

	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["IsError"] = true
	c.Env["Title"] = "Decred VSP - Error"
	c.Env["RateLimited"] = r.URL.Query().Get("rl")

	widgets := controller.Parse(t, "error", c.Env)
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Index renders the home page.
func (controller *MainController) Index(c web.C, r *http.Request) (string, int) {
	if controller.Cfg.ClosePool {
		c.Env["IsClosed"] = true
		c.Env["ClosePoolMsg"] = controller.Cfg.ClosePoolMsg
	}
	c.Env["Network"] = controller.Cfg.NetParams.Name
	c.Env["PoolEmail"] = controller.Cfg.PoolEmail
	c.Env["PoolFees"] = controller.Cfg.PoolFees
	c.Env["CustomDescription"] = controller.Cfg.Description
	c.Env["PoolLink"] = controller.Cfg.PoolLink

	gsi, err := controller.Cfg.StakepooldServers.GetStakeInfo()
	if err != nil {
		log.Errorf("RPC GetStakeInfo failed: %v", err)
		return "/error", http.StatusSeeOther
	}

	c.Env["StakeInfo"] = gsi
	c.Env["LivePercent"] = gsi.ProportionLive * 100

	t := controller.GetTemplate(c)

	// execute the named template with data in c.Env
	widgets, err := helpers.Parse(t, "home", c.Env)
	if err != nil {
		log.Errorf("helpers.Parse: home failed: %v", err)
		return "/error", http.StatusSeeOther
	}
	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["IsIndex"] = true
	c.Env["Title"] = "Decred Voting Service - Welcome"
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)

	doc, err := helpers.Parse(t, "main", c.Env)
	if err != nil {
		log.Errorf("helpers.Parse: main failed: %v", err)
		return "/error", http.StatusSeeOther
	}
	return doc, http.StatusOK
}

// PasswordReset renders the password reset page. This shows the form where the
// user enters their email address.
func (controller *MainController) PasswordReset(c web.C, r *http.Request) (string, int) {
	c.Env["Title"] = "Decred Voting Service - Password Reset"
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	c.Env["FlashError"] = session.Flashes("passwordresetError")
	c.Env["FlashSuccess"] = session.Flashes("passwordresetSuccess")
	c.Env["IsPasswordReset"] = true
	c.Env["CaptchaID"] = captcha.New()
	c.Env["CaptchaMsg"] = "To reset your password, first complete the captcha:"
	c.Env["CaptchaError"] = session.Flashes("captchaFailed")

	t := controller.GetTemplate(c)
	widgets := controller.Parse(t, "passwordreset", c.Env)
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// PasswordResetPost handles the posted password reset form. This submits the
// data entered into the email address form. If the email is recognized a
// password reset token is generated and the user will check their email for a
// link. The link will take them to the password update page with a token
// specified on the URL.
func (controller *MainController) PasswordResetPost(c web.C, r *http.Request) (string, int) {
	email := r.FormValue("email")
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)

	if !controller.IsCaptchaDone(c) {
		session.AddFlash("You must complete the captcha.", "passwordresetError")
		return controller.PasswordReset(c, r)
	}
	session.Values["CaptchaDone"] = false
	c.Env["CaptchaDone"] = false

	remoteIP := getClientIP(r, controller.Cfg.RealIPHeader)
	user, err := helpers.EmailExists(dbMap, email)
	if err == nil {
		log.Infof("PasswordReset POST from %v, email %v", remoteIP,
			user.Email)

		t := time.Now()
		expires := t.Add(time.Hour * 1)

		token := models.NewUserToken()
		passReset := &models.PasswordReset{
			UserId:  user.Id,
			Token:   token.String(),
			Created: t.Unix(),
			Expires: expires.Unix(),
		}

		if err := models.InsertPasswordReset(dbMap, passReset); err != nil {
			session.AddFlash("Unable to add reset token to database", "passwordresetError")
			log.Errorf("Unable to add reset token to database: %v", err)
			return controller.PasswordReset(c, r)
		}

		err := controller.Cfg.EmailSender.PasswordChangeRequest(user.Email, remoteIP, controller.Cfg.BaseURL, token.String())
		if err != nil {
			session.AddFlash("Unable to send password reset email", "passwordresetError")
			log.Errorf("error sending password reset email %v", err)
			return controller.PasswordReset(c, r)
		}
	} else {
		log.Infof("request to reset non-existent account %v from IP %v",
			email, remoteIP)
	}

	session.AddFlash("An email containing password reset instructions has "+
		"been sent to "+email+" if it was a registered account here.",
		"passwordresetSuccess")

	return controller.PasswordReset(c, r)
}

// PasswordUpdate renders the password update page. When a user clicks the link
// containing a token in the password reset email, this handler will validate
// the token. When the token is valid, the user will be presented with forms to
// enter their new password. PasswordUpdatePost handles the submission of these
// forms, and calls PasswordUpdate again for page rendering.
func (controller *MainController) PasswordUpdate(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)

	render := func() string {
		c.Env["Title"] = "Decred Voting Service - Password Update"
		c.Env["FlashError"] = session.Flashes("passwordupdateError")
		c.Env["FlashSuccess"] = session.Flashes("passwordupdateSuccess")
		c.Env["IsPasswordUpdate"] = true
		widgets := controller.Parse(t, "passwordupdate", c.Env)
		c.Env["Designation"] = controller.Cfg.Designation

		c.Env["Content"] = template.HTML(widgets)
		return controller.Parse(t, "main", c.Env)
	}

	// Just render the page if the POST handler already checked the token.
	_, tokenChecked := c.Env["TokenValid"].(bool)
	if tokenChecked {
		return render(), http.StatusOK
	}

	// Use CheckPasswordResetToken to set relevant flash messages.
	controller.CheckPasswordResetToken(r.URL.Query().Get("t"), c)
	return render(), http.StatusOK
}

// PasswordUpdatePost handles updating passwords. The token in the URL is from
// the password reset email. The token is validated and the password is changed.
func (controller *MainController) PasswordUpdatePost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)
	remoteIP := getClientIP(r, controller.Cfg.RealIPHeader)

	// Ensure a valid password reset token is provided. If the token is valid,
	// return the decoded UserToken and PasswordReset data for the token.
	token, resetData, tokenOK := controller.CheckPasswordResetToken(
		r.URL.Query().Get("t"), c)
	c.Env["TokenValid"] = tokenOK
	// tokenChecked will be true regardless of token validity.
	if !tokenOK {
		return controller.PasswordUpdate(c, r)
	}

	// Given a valid password reset token, process the password change.
	password := r.FormValue("password")
	if password == "" {
		session.AddFlash("Password cannot be empty.", "passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}
	passwordRepeat := r.FormValue("passwordrepeat")
	if password != passwordRepeat {
		session.AddFlash("Passwords do not match.", "passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	user, err := helpers.UserIDExists(dbMap, resetData.UserId)
	if err != nil {
		log.Infof("UserIDExists failure %v, %v", err, remoteIP)
		session.AddFlash("Unable to find User ID.", "passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	log.Infof("PasswordUpdate POST from %v, email %v", remoteIP,
		user.Email)

	user.HashPassword(password)
	_, err = helpers.UpdateUserPasswordById(dbMap, resetData.UserId,
		user.Password)
	if err != nil {
		log.Errorf("error updating password %v", err)
		session.AddFlash("Unable to update password.", "passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	err = helpers.PasswordResetTokenDelete(dbMap, token)
	if err != nil {
		log.Errorf("error deleting token %v", err)
	}

	// destroy session data
	if err := system.DestroySessionsForUserID(dbMap, user.Id); err != nil {
		log.Warnf("PasswordUpdatePost: DestroySessionsForUserID '%v' failed: %v",
			user.Id, err)
	}
	session.AddFlash("Password successfully updated", "passwordupdateSuccess")
	return controller.PasswordUpdate(c, r)
}

// Settings renders the settings page.
func (controller *MainController) Settings(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["FlashError"] = session.Flashes("settingsError")
	c.Env["FlashSuccess"] = session.Flashes("settingsSuccess")
	c.Env["IsSettings"] = true
	c.Env["CaptchaID"] = captcha.New()
	c.Env["CaptchaMsg"] = "To change your email address, first complete the captcha:"
	c.Env["CaptchaError"] = session.Flashes("captchaFailed")

	t := controller.GetTemplate(c)
	widgets := controller.Parse(t, "settings", c.Env)

	c.Env["Title"] = "Decred Voting Service - Settings"
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)
	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// SettingsPost handles changing the user's email address or password.
func (controller *MainController) SettingsPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)
	remoteIP := getClientIP(r, controller.Cfg.RealIPHeader)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	password, updateEmail, updatePassword := r.FormValue("password"),
		r.FormValue("updateEmail"), r.FormValue("updatePassword")

	if updateEmail == "true" {
		if !controller.IsCaptchaDone(c) {
			session.AddFlash("You must complete the captcha.", "settingsError")
			return controller.Settings(c, r)
		}
		session.Values["CaptchaDone"] = false
		c.Env["CaptchaDone"] = false
	}

	// Changes to email or password require the current password.
	user, err := helpers.PasswordValidById(dbMap, session.Values["UserId"].(int64), password)
	if err != nil {
		session.AddFlash("Password not valid", "settingsError")
		return controller.Settings(c, r)
	}

	log.Infof("Settings POST from %v, email %v", remoteIP, user.Email)

	if updateEmail == "true" {
		newEmail := r.FormValue("email")
		log.Infof("user requested email change from %v to %v", user.Email, newEmail)

		userExists := models.GetUserByEmail(dbMap, newEmail)
		if userExists != nil {
			session.AddFlash("Email address in use", "settingsError")
			return controller.Settings(c, r)
		}

		t := time.Now()
		expires := t.Add(time.Hour * 1)

		token := models.NewUserToken()
		emailChange := &models.EmailChange{
			UserId:   user.Id,
			NewEmail: newEmail,
			Token:    token.String(),
			Created:  t.Unix(),
			Expires:  expires.Unix(),
		}

		if err := models.InsertEmailChange(dbMap, emailChange); err != nil {
			session.AddFlash("Unable to add email change token to database", "settingsError")
			log.Errorf("Unable to add email change token to database: %v", err)
			return controller.Settings(c, r)
		}

		err = controller.Cfg.EmailSender.EmailChangeVerification(controller.Cfg.BaseURL, user.Email, newEmail, remoteIP, token.String())
		if err != nil {
			session.AddFlash("Unable to send email change token.",
				"settingsError")
			log.Errorf("error sending email change token to new address %v %v",
				newEmail, err)
		} else {
			session.AddFlash("Verification token sent to new email address",
				"settingsSuccess")
		}

		err = controller.Cfg.EmailSender.EmailChangeNotification(controller.Cfg.BaseURL, user.Email, newEmail, remoteIP)
		// inform the user.
		if err != nil {
			log.Errorf("error sending email change token to old address %v %v",
				user.Email, err)
			session.AddFlash("Failed to send email change verification email. "+
				"Please contact the site admin.", "settingsError")
		}
	} else if updatePassword == "true" {
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

		// destroy session data
		err := system.DestroySessionsForUserID(dbMap, user.Id)
		if err != nil {
			log.Warnf("SettingsPost: DestroySessionsForUserId '%v' failed: %v", user.Id, err)
		}

		// send a confirmation email.
		err = controller.Cfg.EmailSender.PasswordChangeConfirm(user.Email, controller.Cfg.BaseURL, remoteIP)
		if err != nil {
			log.Errorf("error sending password change confirmation %v %v",
				user.Email, err)
			session.AddFlash("Failed to send password change email. "+
				"Please contact the site admin.", "settingsError")
		} else {
			session.AddFlash("Password successfully updated", "settingsSuccess")
		}
	}

	return controller.Settings(c, r)
}

// Login renders the login page.
func (controller *MainController) Login(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)

	// Tell main.html what route is being rendered
	c.Env["isLogin"] = true

	c.Env["FlashError"] = session.Flashes("loginError")

	widgets := controller.Parse(t, "auth/login", c.Env)

	c.Env["Title"] = "Decred VSP - Login"
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// LoginPost is the form submit route. Logs user in or sets an appropriate message in
// session if login was not successful.
func (controller *MainController) LoginPost(c web.C, r *http.Request) (string, int) {
	email, password := r.FormValue("email"), r.FormValue("password")

	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)
	remoteIP := getClientIP(r, controller.Cfg.RealIPHeader)

	// Validate email and password combination.
	user, err := helpers.Login(dbMap, email, password)
	if err != nil {
		log.Infof(email+" login failed %v, %v", err, remoteIP)
		session.AddFlash("Invalid Email or Password", "loginError")
		return controller.Login(c, r)
	}

	log.Infof("Login POST from %v, email %v", remoteIP, user.Email)

	if user.EmailVerified == 0 {
		session.AddFlash("You must validate your email address", "loginError")
		return controller.Login(c, r)
	}

	session.Values["UserId"] = user.Id

	// Go to Address page if multisig script not yet set up.
	// GUI users can copy their API Token from here.
	// CLI users can paste their pubkey address
	if user.MultiSigAddress == "" {
		return "/address", http.StatusSeeOther
	}

	// Go to Tickets page if user already set up.
	return "/tickets", http.StatusSeeOther
}

// Register renders the register page.
func (controller *MainController) Register(c web.C, r *http.Request) (string, int) {
	// Tell main.html what route is being rendered
	c.Env["isRegister"] = true
	if controller.Cfg.ClosePool {
		c.Env["IsClosed"] = true
		c.Env["ClosePoolMsg"] = controller.Cfg.ClosePoolMsg
	}

	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	c.Env["FlashError"] = session.Flashes("registrationError")
	c.Env["FlashSuccess"] = session.Flashes("registrationSuccess")
	c.Env["CaptchaID"] = captcha.New()
	c.Env["CaptchaMsg"] = "To register, first complete the captcha:"
	c.Env["CaptchaError"] = session.Flashes("captchaFailed")

	t := controller.GetTemplate(c)
	widgets := controller.Parse(t, "auth/register", c.Env)

	c.Env["Title"] = "Decred VSP - Register"
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)
	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// RegisterPost form submit route. Registers new user or shows Registration route with
// appropriate messages set in session.
func (controller *MainController) RegisterPost(c web.C, r *http.Request) (string, int) {
	if controller.Cfg.ClosePool {
		log.Infof("attempt to register while registration disabled")
		return "/error", http.StatusSeeOther
	}

	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	if !controller.IsCaptchaDone(c) {
		session.AddFlash("You must complete the captcha.", "registrationError")
		return controller.Register(c, r)
	}

	remoteIP := getClientIP(r, controller.Cfg.RealIPHeader)

	email, password, passwordRepeat := r.FormValue("email"),
		r.FormValue("password"), r.FormValue("passwordrepeat")

	if !strings.Contains(email, "@") {
		session.AddFlash("Email address is invalid", "registrationError")
		return controller.Register(c, r)
	}

	if password == "" {
		session.AddFlash("Password cannot be empty", "registrationError")
		return controller.Register(c, r)
	}

	if password != passwordRepeat {
		session.AddFlash("Passwords do not match", "registrationError")
		return controller.Register(c, r)
	}

	// At this point we have completed all trivial pre-registration checks. The new account
	// is about to be created, so lets consume the CAPTCHA. Any failure beyond this point
	// and we want the user to complete another CAPTCHA.
	session.Values["CaptchaDone"] = false
	c.Env["CaptchaDone"] = false

	dbMap := controller.GetDbMap(c)
	user := models.GetUserByEmail(dbMap, email)

	if user != nil {
		session.AddFlash("This email address is already registered", "registrationError")
		return controller.Register(c, r)
	}

	token := models.NewUserToken()
	user = &models.User{
		Username:        email,
		Email:           email,
		EmailToken:      token.String(),
		EmailVerified:   0,
		VoteBits:        1,
		VoteBitsVersion: int64(controller.voteVersion),
	}
	user.HashPassword(password)

	log.Infof("Register POST from %v, email %v. Inserting.", remoteIP, user.Email)

	err := models.InsertUser(dbMap, user)
	if err != nil {
		session.AddFlash("Database error occurred while adding user", "registrationError")
		log.Errorf("Error while registering user: %v", err)
		return controller.Register(c, r)
	}

	err = controller.Cfg.EmailSender.Registration(email, controller.Cfg.BaseURL, remoteIP, token.String())
	if err != nil {
		session.AddFlash("Unable to send verification email", "registrationError")
		log.Errorf("error sending verification email %v", err)
	} else {
		session.AddFlash("A verification email has been sent to "+email, "registrationSuccess")
	}

	return controller.Register(c, r)
}

// Stats renders the stats page.
func (controller *MainController) Stats(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["IsStats"] = true
	c.Env["Title"] = "Decred VSP - Stats"

	dbMap := controller.GetDbMap(c)

	userCount := models.GetUserCount(dbMap)
	userCountActive := models.GetUserCountActive(dbMap)

	gsi, err := controller.Cfg.StakepooldServers.GetStakeInfo()
	if err != nil {
		log.Errorf("RPC GetStakeInfo failed: %v", err)
		return "/error", http.StatusSeeOther
	}

	c.Env["DCRDataURL"] = controller.DCRDataURL

	c.Env["PoolEmail"] = controller.Cfg.PoolEmail
	c.Env["PoolFees"] = controller.Cfg.PoolFees
	c.Env["StakeInfo"] = gsi
	c.Env["UserCount"] = userCount
	c.Env["UserCountActive"] = userCountActive

	widgets := controller.Parse(t, "stats", c.Env)
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// ByTicketHeight type implements sort.Sort for types with a TicketHeight field.
// This includes all valid tickets, including spend tickets.
type ByTicketHeight []TicketInfo

func (a ByTicketHeight) Len() int {
	return len(a)
}
func (a ByTicketHeight) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}
func (a ByTicketHeight) Less(i, j int) bool {
	return a[i].TicketHeight < a[j].TicketHeight
}

// BySpentByHeight type implements sort.Sort for types with a SpentByHeight
// field, namely TicketInfoHistoric, the type for voted/missed/expired tickets.
type BySpentByHeight []TicketInfoHistoric

func (a BySpentByHeight) Len() int {
	return len(a)
}
func (a BySpentByHeight) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}
func (a BySpentByHeight) Less(i, j int) bool {
	return a[i].SpentByHeight < a[j].SpentByHeight
}

// TicketInfoHistoric represents spent tickets, either voted or revoked.
type TicketInfoHistoric struct {
	Ticket        string
	SpentBy       string
	SpentByHeight uint32
	TicketHeight  uint32
}

// TicketInfoInvalid represents tickets that were not added by the wallets for
// any reason (e.g. incorrect subsidy address or pool fees).
type TicketInfoInvalid struct {
	Ticket string
}

// TicketInfo represents live or immature tickets that have yet to
// be spent by either a vote or revocation.
type TicketInfo struct {
	TicketHeight uint32
	Ticket       string
}

// Tickets renders the tickets page.
func (controller *MainController) Tickets(c web.C, r *http.Request) (string, int) {
	var ticketInfoInvalid []TicketInfoInvalid
	var ticketInfoLive, ticketInfoImmature []TicketInfo
	var ticketInfoVoted, ticketInfoExpired, ticketInfoMissed []TicketInfoHistoric
	var numVoted int

	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	remoteIP := getClientIP(r, controller.Cfg.RealIPHeader)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	c.Env["IsTickets"] = true
	c.Env["DCRDataURL"] = controller.DCRDataURL
	c.Env["Title"] = "Decred VSP - Tickets"

	dbMap := controller.GetDbMap(c)
	user, _ := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	if user.MultiSigAddress == "" {
		log.Info("Multisigaddress empty")
		return "/address", http.StatusSeeOther
	}

	// Get P2SH Address
	multisig, err := dcrutil.DecodeAddress(user.MultiSigAddress, controller.Cfg.NetParams)
	if err != nil {
		log.Warnf("Invalid address %v in database: %v", user.MultiSigAddress, err)
		return "/error", http.StatusSeeOther
	}

	log.Infof("Tickets GET from %v, multisig %v", remoteIP,
		user.MultiSigAddress)

	start := time.Now()

	spui, err := controller.Cfg.StakepooldServers.StakePoolUserInfo(multisig.String())
	if err != nil {
		// Render page with message to try again later
		log.Errorf("RPC StakePoolUserInfo failed: %v", err)
		return "/error", http.StatusSeeOther
	}

	log.Debugf(":: StakePoolUserInfo (msa = %v) execution time: %v",
		user.MultiSigAddress, time.Since(start))

	// If the user has tickets, get their info
	if spui != nil && len(spui.Tickets) > 0 {
		for _, ticket := range spui.Tickets {
			switch ticket.Status {
			case "immature":
				ticketInfoImmature = append(ticketInfoImmature, TicketInfo{
					TicketHeight: ticket.TicketHeight,
					Ticket:       ticket.Ticket,
				})
			case "live":
				ticketInfoLive = append(ticketInfoLive, TicketInfo{
					TicketHeight: ticket.TicketHeight,
					Ticket:       ticket.Ticket,
				})

			case "expired":
				ticketInfoExpired = append(ticketInfoExpired, TicketInfoHistoric{
					Ticket:        ticket.Ticket,
					SpentByHeight: ticket.SpentByHeight,
					TicketHeight:  ticket.TicketHeight,
				})
			case "missed":
				ticketInfoMissed = append(ticketInfoMissed, TicketInfoHistoric{
					Ticket:        ticket.Ticket,
					SpentByHeight: ticket.SpentByHeight,
					TicketHeight:  ticket.TicketHeight,
				})
			case "voted":
				ticketInfoVoted = append(ticketInfoVoted, TicketInfoHistoric{
					Ticket:        ticket.Ticket,
					SpentBy:       ticket.SpentBy,
					SpentByHeight: ticket.SpentByHeight,
					TicketHeight:  ticket.TicketHeight,
				})
			}
		}
	}

	numVoted = len(ticketInfoVoted)

	if spui != nil && len(spui.InvalidTickets) > 0 {
		for _, ticket := range spui.InvalidTickets {
			ticketInfoInvalid = append(ticketInfoInvalid, TicketInfoInvalid{ticket})
		}
	}

	// Sort tickets for display. Ideally these would be sorted on the front
	// end by javascript.
	sort.Sort(sort.Reverse(ByTicketHeight(ticketInfoLive)))
	sort.Sort(sort.Reverse(ByTicketHeight(ticketInfoImmature)))
	sort.Sort(sort.Reverse(BySpentByHeight(ticketInfoExpired)))
	sort.Sort(sort.Reverse(BySpentByHeight(ticketInfoVoted)))
	sort.Sort(sort.Reverse(BySpentByHeight(ticketInfoMissed)))

	// Truncate the slice of voted tickets if there are too many
	if len(ticketInfoVoted) > controller.Cfg.MaxVotedTickets {
		ticketInfoVoted = ticketInfoVoted[0:controller.Cfg.MaxVotedTickets]
	}

	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["TicketsInvalid"] = ticketInfoInvalid
	c.Env["TicketsImmature"] = ticketInfoImmature
	c.Env["TicketsLive"] = ticketInfoLive
	c.Env["TicketsExpired"] = ticketInfoExpired
	c.Env["TicketsMissed"] = ticketInfoMissed
	c.Env["TicketsVotedCount"] = numVoted
	c.Env["TicketsVotedMaxDisplay"] = controller.Cfg.MaxVotedTickets
	c.Env["TicketsVoted"] = ticketInfoVoted
	widgets := controller.Parse(t, "tickets", c.Env)

	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)
	c.Env["Flash"] = session.Flashes("tickets")

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Voting renders the voting page.
func (controller *MainController) Voting(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	user, _ := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	if user.MultiSigAddress == "" {
		log.Info("Multisigaddress empty")
		return "/address", http.StatusSeeOther
	}

	t := controller.GetTemplate(c)

	choicesSelected := controller.choicesForAgendas(uint16(user.VoteBits))

	for k, v := range choicesSelected {
		strk := strconv.Itoa(k)
		c.Env["Agenda"+strk+"Selected"] = v
	}
	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["Agendas"] = controller.agendas()
	c.Env["FlashError"] = session.Flashes("votingError")
	c.Env["FlashSuccess"] = session.Flashes("votingSuccess")
	c.Env["IsVoting"] = true
	c.Env["VoteVersion"] = controller.voteVersion

	widgets := controller.Parse(t, "voting", c.Env)
	c.Env["Title"] = "Decred Voting Service - Voting"
	c.Env["Designation"] = controller.Cfg.Designation

	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// VotingPost form submit route.
func (controller *MainController) VotingPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	dbMap := controller.GetDbMap(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	var generatedVoteBits uint16

	user, _ := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	// last block valid
	generatedVoteBits |= 1

	deployments := controller.getAgendas()

	for i := range deployments {
		agendaVal := r.FormValue("agenda" + strconv.Itoa(i))
		avi, err := strconv.Atoi(agendaVal)
		if err != nil {
			session.AddFlash("invalid agenda choice", "votingError")
			return "/voting", http.StatusSeeOther
		}
		generatedVoteBits |= uint16(avi)
	}

	isValid := controller.IsValidVoteBits(generatedVoteBits)
	if !isValid {
		session.AddFlash("generated votebits were invalid", "votingError")
		return "/voting", http.StatusSeeOther
	}

	oldVoteBits := user.VoteBits
	user, err := helpers.UpdateVoteBitsByID(dbMap, user.Id, generatedVoteBits)
	if err != nil {
		session.AddFlash("unable to save new voting preferences", "votingError")
		return "/voting", http.StatusSeeOther
	}

	log.Infof("updated voteBits for user %d from %d to %d",
		user.Id, oldVoteBits, generatedVoteBits)
	if uint16(oldVoteBits) != generatedVoteBits {
		if err := controller.StakepooldUpdateUsers(dbMap); err != nil {
			log.Errorf("unable to update all: %v", err)
		}
	}

	session.AddFlash("Successfully updated voting preferences", "votingSuccess")
	return "/voting", http.StatusSeeOther
}

// Logout the user.
func (controller *MainController) Logout(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	c.Env[csrf.TemplateTag] = csrf.TemplateField(r)
	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	session.Options.MaxAge = -1

	return "/", http.StatusSeeOther
}

func (controller *MainController) choicesForAgendas(userVoteBits uint16) map[int]uint16 {
	choicesSelected := make(map[int]uint16)

	deployments := controller.getAgendas()

	for i := range deployments {
		d := &deployments[i]
		masked := userVoteBits & d.Vote.Mask
		var valid bool
		for choice := range d.Vote.Choices {
			if masked == d.Vote.Choices[choice].Bits {
				valid = true
				choicesSelected[i] = d.Vote.Choices[choice].Bits
			}
		}
		if !valid {
			choicesSelected[i] = uint16(0)
		}
	}

	return choicesSelected
}

func (controller *MainController) getAgendas() []chaincfg.ConsensusDeployment {
	if controller.Cfg.NetParams.Deployments == nil {
		return nil
	}
	return controller.Cfg.NetParams.Deployments[controller.voteVersion]
}

// CalcEstimatedTicketExpiry returns a time.Time reflecting the estimated time
// that the ticket will expire.  A safety margin of 5% padding is applied to
// ensure the ticket is not removed prematurely.
// XXX we should really be using the actual expiry height of the ticket instead
// of an estimated time but the stakepool doesn't have a way to retrieve that
// information yet.
func (controller *MainController) CalcEstimatedTicketExpiry() time.Time {
	t := time.Now()

	// Generate an estimated expiry time for this ticket and add 5% for
	// a margin of safety in case blocks are slower than expected
	minutesUntilExpiryEstimate := time.Duration(controller.Cfg.NetParams.TicketExpiry) * controller.Cfg.NetParams.TargetTimePerBlock
	expires := t.Add(minutesUntilExpiryEstimate * 105 / 100)

	return expires
}

// IsValidVoteBits returns an error if voteBits are not valid for agendas
func (controller *MainController) IsValidVoteBits(userVoteBits uint16) bool {
	// All blocks valid is OK
	if userVoteBits == 1 {
		return true
	}

	// check if last block invalid is set at all
	if userVoteBits&1 == 0 {
		return false
	}

	usedBits := uint16(1)
	deployments := controller.getAgendas()
	for i := range deployments {
		d := &deployments[i]
		masked := userVoteBits & d.Vote.Mask
		var valid bool
		for choice := range d.Vote.Choices {
			usedBits |= d.Vote.Choices[choice].Bits
			if masked == d.Vote.Choices[choice].Bits {
				valid = true
			}
		}
		if !valid {
			return false
		}
	}

	return userVoteBits&^usedBits == 0
}

func stringSliceContains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
