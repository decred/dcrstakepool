package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/dchest/captcha"
	"html/template"
	"net"
	"net/http"
	"net/smtp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/hdkeychain"
	"github.com/decred/dcrstakepool/helpers"
	"github.com/decred/dcrstakepool/models"
	"github.com/decred/dcrstakepool/poolapi"
	"github.com/decred/dcrstakepool/stakepooldclient"
	"github.com/decred/dcrstakepool/system"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/go-gorp/gorp"
	"github.com/zenazn/goji/web"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
)

var (
	// MaxUsers is the maximum number of users supported by a voting service.
	// This is an artificial limit and can be increased by adjusting the
	// ticket/fee address indexes above 10000.
	MaxUsers            = 10000
	signupEmailSubject  = "Voting service provider email verification"
	signupEmailTemplate = "A request for an account for __URL__\r\n" +
		"was made from __REMOTEIP__ for this email address.\r\n\n" +
		"If you made this request, follow the link below:\r\n\n" +
		"__URL__/emailverify?t=__TOKEN__\r\n\n" +
		"to verify your email address and finalize registration.\r\n\n"
	StakepooldUpdateKindAll     = "ALL"
	StakepooldUpdateKindUsers   = "USERS"
	StakepooldUpdateKindTickets = "TICKETS"
)

// MainController is the wallet RPC controller type.  Its methods include the
// route handlers.
type MainController struct {
	// embed type for c.Env[""] context and ExecuteTemplate helpers
	system.Controller

	adminIPs             []string
	adminUserIDs         []string
	APISecret            string
	APIVersionsSupported []int
	baseURL              string
	closePool            bool
	closePoolMsg         string
	enableStakepoold     bool
	feeXpub              *hdkeychain.ExtendedKey
	grpcConnections      []*grpc.ClientConn
	poolEmail            string
	poolFees             float64
	poolLink             string
	params               *chaincfg.Params
	rpcServers           *walletSvrManager
	realIPHeader         string
	captchaHandler       *CaptchaHandler
	smtpFrom             string
	smtpHost             string
	smtpUsername         string
	smtpPassword         string
	version              string
	voteVersion          uint32
	votingXpub           *hdkeychain.ExtendedKey
	maxVotedAge          int64
}

func randToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
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
func NewMainController(params *chaincfg.Params, adminIPs []string,
	adminUserIDs []string, APISecret string, APIVersionsSupported []int,
	baseURL string, closePool bool, closePoolMsg string, enablestakepoold bool,
	feeXpubStr string, grpcConnections []*grpc.ClientConn, poolFees float64,
	poolEmail, poolLink, smtpFrom, smtpHost, smtpUsername, smtpPassword,
	version string, walletHosts, walletCerts, walletUsers,
	walletPasswords []string, minServers int, realIPHeader,
	votingXpubStr string, maxVotedAge int64) (*MainController, error) {

	// Parse the extended public key and the pool fees.
	feeKey, err := hdkeychain.NewKeyFromString(feeXpubStr)
	if err != nil {
		return nil, err
	}
	if !feeKey.IsForNet(params) {
		return nil, fmt.Errorf("fee extended public key is for wrong network")
	}

	// Parse the extended public key for the voting addresses.
	voteKey, err := hdkeychain.NewKeyFromString(votingXpubStr)
	if err != nil {
		return nil, err
	}
	if !voteKey.IsForNet(params) {
		return nil, fmt.Errorf("voting extended public key is for wrong network")
	}

	rpcs, err := newWalletSvrManager(walletHosts, walletCerts, walletUsers, walletPasswords, minServers)
	if err != nil {
		return nil, err
	}

	ch := &CaptchaHandler{
		ImgHeight: 127,
		ImgWidth:  257,
	}

	mc := &MainController{
		adminIPs:             adminIPs,
		adminUserIDs:         adminUserIDs,
		APISecret:            APISecret,
		APIVersionsSupported: APIVersionsSupported,
		baseURL:              baseURL,
		closePool:            closePool,
		closePoolMsg:         closePoolMsg,
		enableStakepoold:     enablestakepoold,
		feeXpub:              feeKey,
		grpcConnections:      grpcConnections,
		poolEmail:            poolEmail,
		poolFees:             poolFees,
		poolLink:             poolLink,
		params:               params,
		captchaHandler:       ch,
		rpcServers:           rpcs,
		realIPHeader:         realIPHeader,
		smtpFrom:             smtpFrom,
		smtpHost:             smtpHost,
		smtpUsername:         smtpUsername,
		smtpPassword:         smtpPassword,
		version:              version,
		votingXpub:           voteKey,
		maxVotedAge:          maxVotedAge,
	}

	voteVersion, err := mc.GetVoteVersion()
	if err != nil || voteVersion == 0 {
		cErr := fmt.Errorf("Failed to get wallets' Vote Version: %v", err)
		return nil, cErr
	}

	mc.voteVersion = voteVersion

	return mc, nil
}

// getNetworkName will strip any suffix from a network name starting with
// "testnet" (e.g. "testnet3"). This is primarily intended for the tickets page,
// which generates block explorer links using a value set by the network string,
// which is a problem since there is no testnet3.dcrdata.org host.
func (controller *MainController) getNetworkName() string {
	if strings.HasPrefix(controller.params.Name, "testnet") {
		return "testnet"
	}
	return controller.params.Name
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

	if len(userPubKeyAddr) < 40 {
		return nil, codes.InvalidArgument, "address error", errors.New("address too short")
	}

	if len(userPubKeyAddr) > 65 {
		return nil, codes.InvalidArgument, "address error", errors.New("address too long")
	}

	u, err := dcrutil.DecodeAddress(userPubKeyAddr)
	if err != nil {
		return nil, codes.InvalidArgument, "address error", errors.New("couldn't decode address")
	}

	_, is := u.(*dcrutil.AddressSecpPubKey)
	if !is {
		return nil, codes.InvalidArgument, "address error", errors.New("incorrect address type")
	}

	// Get the ticket address for this user
	pooladdress, err := controller.TicketAddressForUserID(int(c.Env["APIUserID"].(int64)))
	if err != nil {
		log.Errorf("unable to derive ticket address: %v", err)
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	poolValidateAddress, err := controller.rpcServers.ValidateAddress(pooladdress)
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

	p, err := dcrutil.DecodeAddress(poolPubKeyAddr)
	if err != nil {
		controller.handlePotentialFatalError("DecodeAddress poolPubKeyAddr", err)
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	if controller.RPCIsStopped() {
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}
	createMultiSig, err := controller.rpcServers.CreateMultisig(1, []dcrutil.Address{p, u})
	if err != nil {
		controller.handlePotentialFatalError("CreateMultisig", err)
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	if controller.RPCIsStopped() {
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}
	_, bestBlockHeight, err := controller.rpcServers.GetBestBlock()
	if err != nil {
		controller.handlePotentialFatalError("GetBestBlock", err)
	}

	if controller.RPCIsStopped() {
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}
	serializedScript, err := hex.DecodeString(createMultiSig.RedeemScript)
	if err != nil {
		controller.handlePotentialFatalError("CreateMultisig DecodeString", err)
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}
	err = controller.rpcServers.ImportScript(serializedScript, int(bestBlockHeight))
	if err != nil {
		controller.handlePotentialFatalError("ImportScript", err)
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	userFeeAddr, err := controller.FeeAddressForUserID(int(user.Id))
	if err != nil {
		log.Warnf("unexpected error deriving pool addr: %s", err.Error())
		return nil, codes.Unavailable, "system error", errors.New("unable to process wallet commands")
	}

	models.UpdateUserByID(dbMap, user.Id, createMultiSig.Address,
		createMultiSig.RedeemScript, poolPubKeyAddr, userPubKeyAddr,
		userFeeAddr.EncodeAddress(), bestBlockHeight)

	log.Infof("successfully create multisigaddress for user %d", c.Env["APIUserID"])
	controller.StakepooldUpdateAll(dbMap, StakepooldUpdateKindUsers)

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
		PoolFees:      controller.poolFees,
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

	if controller.RPCIsStopped() {
		return nil, codes.Unavailable, "stats error", errors.New("RPC server stopped")
	}

	gsi, err := controller.rpcServers.GetStakeInfo()
	if err != nil {
		log.Infof("RPC GetStakeInfo failed: %v", err)
		return nil, codes.Unavailable, "stats error", errors.New("RPC server error")
	}

	var poolStatus string
	if controller.closePool {
		poolStatus = "Closed"
	} else {
		poolStatus = "Open"
	}

	stats := &poolapi.Stats{
		AllMempoolTix:        gsi.AllMempoolTix,
		APIVersionsSupported: controller.APIVersionsSupported,
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
		Network:              controller.params.Name,
		PoolEmail:            controller.poolEmail,
		PoolFees:             controller.poolFees,
		PoolStatus:           poolStatus,
		UserCount:            userCount,
		UserCountActive:      userCountActive,
		Version:              controller.version,
	}

	return stats, codes.OK, "stats successfully retrieved", nil
}

// APIVotingPost is the API version of VotingPost
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
		controller.StakepooldUpdateAll(dbMap, StakepooldUpdateKindUsers)
	}

	log.Infof("updated voteBits for user %d from %d to %d",
		user.Id, oldVoteBits, userVoteBits)

	return nil, codes.OK, "successfully updated voting preferences", nil
}

func (controller *MainController) isAdmin(c web.C, r *http.Request) (bool, error) {
	remoteIP := getClientIP(r, controller.realIPHeader)
	session := controller.GetSession(c)

	if session.Values["UserId"] == nil {
		return false, fmt.Errorf("%s request with no session from %s",
			r.URL, remoteIP)
	}

	uidstr := strconv.Itoa(int(session.Values["UserId"].(int64)))

	if !stringSliceContains(controller.adminIPs, remoteIP) {
		return false, fmt.Errorf("%s request from %s "+
			"userid %s failed AdminIPs check", r.URL, remoteIP, uidstr)
	}

	if !stringSliceContains(controller.adminUserIDs, uidstr) {
		return false, fmt.Errorf("%s request from %s "+
			"userid %s failed adminUserIDs check", r.URL, remoteIP, uidstr)
	}

	return true, nil
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

// StakepooldGetIgnoredLowFeeTickets performs a gRPC GetIgnoredLowFeeTickets
// request against all stakepoold instances and returns the first result fetched
// without errors
func (controller *MainController) StakepooldGetIgnoredLowFeeTickets() (map[chainhash.Hash]string, error) {
	var err error
	ignoredLowFeeTickets := make(map[chainhash.Hash]string)

	// TODO need some better code here
	for i := range controller.grpcConnections {
		ignoredLowFeeTickets, err = stakepooldclient.StakepooldGetIgnoredLowFeeTickets(controller.grpcConnections[i])
		// take the first non-error result
		if err == nil {
			return ignoredLowFeeTickets, err
		}
	}

	return ignoredLowFeeTickets, err
}

// StakepooldUpdateAll attempts to trigger all connected stakepoold
// instances to pull a data update of the specified kind.
func (controller *MainController) StakepooldUpdateAll(dbMap *gorp.DbMap, updateKind string) error {
	var votableLowFeeTickets []models.LowFeeTicket
	var allUsers map[int64]*models.User
	var err error

	switch updateKind {
	case StakepooldUpdateKindAll, StakepooldUpdateKindTickets, StakepooldUpdateKindUsers:
		// valid
	default:
		return fmt.Errorf("TriggerStakepoolUpdate: unhandled update kind %v",
			updateKind)
	}

	switch updateKind {
	case StakepooldUpdateKindAll, StakepooldUpdateKindTickets:
		votableLowFeeTickets, err = models.GetVotableLowFeeTickets(dbMap)
		if err != nil {
			return err
		}
	}

	switch updateKind {
	case StakepooldUpdateKindAll, StakepooldUpdateKindUsers:
		// reset votebits if Vote Version changed or if the stored VoteBits are
		// somehow invalid
		allUsers, err = controller.CheckAndResetUserVoteBits(dbMap)
		if err != nil {
			return err
		}
	}

	successCount := 0
	for i := range controller.grpcConnections {
		var err error
		var success bool

		switch updateKind {
		case StakepooldUpdateKindAll, StakepooldUpdateKindTickets:
			success, err = stakepooldclient.StakepooldSetAddedLowFeeTickets(controller.grpcConnections[i], votableLowFeeTickets)
			if err != nil {
				log.Errorf("stakepoold host %d unable to update manual "+
					"tickets grpc error: %v", i, err)
			}
			if !success {
				// TODO(maybe) should re-try in the background until we get a
				// successful update
				log.Errorf("stakepoold host %d unable to update manual "+
					"tickets stakepoold update would have blocked", i)
			}
		}

		switch updateKind {
		case StakepooldUpdateKindAll, StakepooldUpdateKindUsers:
			success, err = stakepooldclient.StakepooldSetUserVotingPrefs(controller.grpcConnections[i], allUsers)
			if err != nil {
				log.Errorf("stakepoold host %d unable to update voting config "+
					"grpc error: %v", i, err)
			}
			if !success {
				// TODO(maybe) should re-try in the background until we get a
				// successful update
				log.Errorf("stakepoold host %d unable to update voting config "+
					"stakepoold update would have blocked", i)
			}
		}

		if err == nil {
			log.Infof("successfully triggered update kind %s on stakepoold "+
				"host %d", updateKind, i)
			successCount++
		}
	}

	if successCount == 0 {
		log.Warn("no stakepoold connections alive/working?")
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

	acctKey := controller.feeXpub
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

	addr, err := key.Address(controller.params)
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

	acctKey := controller.votingXpub
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

	addr, err := key.Address(controller.params)
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

	err = walletSvrsSync(controller.rpcServers, multisigScripts)
	return err
}

// GetVoteVersion
func (controller *MainController) GetVoteVersion() (uint32, error) {
	voteVersion, err := checkWalletsVoteVersion(controller.rpcServers)
	if err != nil {
		return 0, err
	}

	return voteVersion, err
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
		} else {
			// Validate that the votebits are valid for the agendas of the current
			// vote version
			if !controller.IsValidVoteBits(uint16(user.VoteBits)) {
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

// handlePotentialFatalError is a helper function to do log possibly
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

	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["IsAddress"] = true
	c.Env["Network"] = controller.getNetworkName()

	c.Env["Flash"] = session.Flashes("address")
	widgets := controller.Parse(t, "address", c.Env)

	c.Env["Title"] = "Decred Stake Pool - Address"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// AddressPost is address form submit route.
func (controller *MainController) AddressPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	remoteIP := getClientIP(r, controller.realIPHeader)

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

	if len(userPubKeyAddr) < 40 {
		session.AddFlash("Address is too short", "address")
		return controller.Address(c, r)
	}

	if len(userPubKeyAddr) > 65 {
		session.AddFlash("Address is too long", "address")
		return controller.Address(c, r)
	}

	// Get dcrutil.Address for user from pubkey address string
	u, err := dcrutil.DecodeAddress(userPubKeyAddr)
	if err != nil {
		session.AddFlash("Couldn't decode address", "address")
		return controller.Address(c, r)
	}

	_, is := u.(*dcrutil.AddressSecpPubKey)
	if !is {
		session.AddFlash("Incorrect address type", "address")
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
	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}
	poolValidateAddress, err := controller.rpcServers.ValidateAddress(pooladdress)
	if err != nil {
		controller.handlePotentialFatalError("ValidateAddress pooladdress", err)
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
	p, err := dcrutil.DecodeAddress(poolPubKeyAddr)
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
		userFeeAddr.EncodeAddress(), bestBlockHeight)

	controller.StakepooldUpdateAll(dbMap, StakepooldUpdateKindUsers)

	return "/tickets", http.StatusSeeOther
}

// AdminStatus renders the status page.
func (controller *MainController) AdminStatus(c web.C, r *http.Request) (string, int) {
	isAdmin, err := controller.isAdmin(c, r)
	if !isAdmin {
		log.Warnf("isAdmin check failed: %v", err)
		return "", http.StatusUnauthorized
	}

	type stakepooldInfoPage struct {
		Status string
	}

	stakepooldPageInfo := make([]stakepooldInfoPage, len(controller.grpcConnections))

	for i, conn := range controller.grpcConnections {
		grpcStatus := "Unknown"
		state := conn.GetState()
		switch state {
		case connectivity.Idle:
			grpcStatus = "Idle"
		case connectivity.Shutdown:
			grpcStatus = "Shutdown"
		case connectivity.Ready:
			grpcStatus = "Ready"
		case connectivity.Connecting:
			grpcStatus = "Connecting"
		case connectivity.TransientFailure:
			grpcStatus = "TransientFailure"
		}
		stakepooldPageInfo[i] = stakepooldInfoPage{
			Status: grpcStatus,
		}
	}

	// Attempt to query wallet statuses
	walletInfo, err := controller.WalletStatus()
	if err != nil {
		log.Errorf("Failed to execute WalletStatus: %v", err)
		return "/error", http.StatusSeeOther
	}

	type WalletInfoPage struct {
		Connected       bool
		DaemonConnected bool
		Unlocked        bool
		EnableVoting    bool
	}
	walletPageInfo := make([]WalletInfoPage, len(walletInfo))
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
			Connected:       true,
			DaemonConnected: v.DaemonConnected,
			EnableVoting:    v.Voting,
			Unlocked:        v.Unlocked,
		}
	}

	// Depending on how many wallets have been detected update RPCStatus.
	// Admins can then use to monitor this page periodically and check status.
	var rpcstatus string
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
	c.Env["Admin"] = isAdmin
	c.Env["IsAdminStatus"] = true
	c.Env["Title"] = "Decred Voting Service - Status (Admin)"

	// Set info to be used by admins on /status page.
	c.Env["StakepooldInfo"] = stakepooldPageInfo
	c.Env["WalletInfo"] = walletPageInfo
	c.Env["RPCStatus"] = rpcstatus

	widgets := controller.Parse(t, "admin/status", c.Env)
	c.Env["Content"] = template.HTML(widgets)

	if controller.RPCIsStopped() {
		return controller.Parse(t, "main", c.Env), http.StatusInternalServerError
	}

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// AdminTickets renders the administrative tickets page.
func (controller *MainController) AdminTickets(c web.C, r *http.Request) (string, int) {
	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
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

	c.Env["Admin"] = isAdmin
	c.Env["IsAdminTickets"] = true
	c.Env["Network"] = controller.getNetworkName()

	c.Env["FlashError"] = session.Flashes("adminTicketsError")
	c.Env["FlashSuccess"] = session.Flashes("adminTicketsSuccess")

	c.Env["AddedLowFeeTickets"] = votableLowFeeTickets
	c.Env["IgnoredLowFeeTickets"], _ = controller.StakepooldGetIgnoredLowFeeTickets()
	widgets := controller.Parse(t, "admin/tickets", c.Env)

	c.Env["Title"] = "Decred Voting Service - Tickets (Admin)"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// AdminTicketsPost validates and processes the form posted from AdminTickets.
func (controller *MainController) AdminTicketsPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)
	remoteIP := getClientIP(r, controller.realIPHeader)

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

	if len(r.PostForm["tickets[]"]) == 0 {
		session.AddFlash("no tickets selected to modify", "adminTicketsError")
		return "/admintickets", http.StatusSeeOther
	}

	switch r.PostFormValue("action") {
	case "Add", "Remove":
		// valid
		break
	default:
		session.AddFlash("invalid or unknown form action type", "adminTicketsError")
		return "/admintickets", http.StatusSeeOther
	}

	actionVerb := "Unknown"
	switch r.PostFormValue("action") {
	case "Add":
		actionVerb = "added"
		ignoredLowFeeTickets, err := controller.StakepooldGetIgnoredLowFeeTickets()
		if err != nil {
			session.AddFlash("GetIgnoredLowFeeTickets error: "+err.Error(), "adminTicketsError")
			return "/admintickets", http.StatusSeeOther
		}

		for _, ticketToAddString := range r.PostForm["tickets[]"] {
			t := time.Now()

			// TODO check if it is already present in the database
			// and error out if so

			tickethash, err := chainhash.NewHashFromStr(ticketToAddString)
			if err != nil {
				session.AddFlash("NewHashFromStr failed for "+ticketToAddString+": "+err.Error(), "adminTicketsError")
				return "/admintickets", http.StatusSeeOther
			}

			msa, exists := ignoredLowFeeTickets[*tickethash]
			if !exists {
				session.AddFlash("ticket " + ticketToAddString + " is no longer present")
				return "/admintickets", http.StatusSeeOther
			}

			expires := controller.CalcEstimatedTicketExpiry()

			lowFeeTicket := &models.LowFeeTicket{
				AddedByUid:    session.Values["UserId"].(int64),
				TicketAddress: msa,
				TicketHash:    ticketToAddString,
				TicketExpiry:  0,
				Voted:         0,
				Created:       t.Unix(),
				Expires:       expires.Unix(),
			}

			if err := models.InsertLowFeeTicket(dbMap, lowFeeTicket); err != nil {
				session.AddFlash("Database error occurred while adding ticket "+ticketToAddString, "adminTicketsError")
				log.Warnf("Adding ticket %v failed: %v", tickethash, err)
				return "/admintickets", http.StatusSeeOther
			}
		}
	case "Remove":
		actionVerb = "removed"
		query := "DELETE FROM LowFeeTicket WHERE TicketHash IN (\"" +
			strings.Join(r.PostForm["tickets[]"], ",") + "\")"
		_, err := dbMap.Exec(query)
		if err != nil {
			session.AddFlash("failed to execute delete query: "+err.Error(), "adminTicketsError")
			return "/admintickets", http.StatusSeeOther
		}
	}

	err = controller.StakepooldUpdateAll(dbMap, StakepooldUpdateKindTickets)
	if err != nil {
		session.AddFlash("StakepooldUpdateAll error: "+err.Error(), "adminTicketsError")
	}

	log.Infof("ip %v userid %v %v for %v ticket(s)",
		remoteIP, session.Values["UserId"].(int64), actionVerb,
		len(r.PostForm["tickets[]"]))
	session.AddFlash("successfully "+actionVerb+" "+
		strconv.Itoa(len(r.PostForm["tickets[]"]))+" ticket(s)", "adminTicketsSuccess")

	return "/admintickets", http.StatusSeeOther
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
	c.Env["Title"] = "Decred Voting Service - Email Update"
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
	c.Env["Title"] = "Decred Voting Service - Email Verification"
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

	c.Env["Admin"], _ = controller.isAdmin(c, r)
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

	// execute the named template with data in c.Env
	widgets := helpers.Parse(t, "home", c.Env)

	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["IsIndex"] = true
	c.Env["Title"] = "Decred Voting Service - Welcome"
	c.Env["Content"] = template.HTML(widgets)

	return helpers.Parse(t, "main", c.Env), http.StatusOK
}

// PasswordReset renders the password reset page.
func (controller *MainController) PasswordReset(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	c.Env["FlashError"] = session.Flashes("passwordresetError")
	c.Env["FlashSuccess"] = session.Flashes("passwordresetSuccess")
	c.Env["IsPasswordReset"] = true
	if controller.smtpHost == "" {
		c.Env["SMTPDisabled"] = true
	}
	c.Env["CaptchaID"] = captcha.New()

	t := controller.GetTemplate(c)
	widgets := controller.Parse(t, "passwordreset", c.Env)

	c.Env["Title"] = "Decred Voting Service - Password Reset"
	c.Env["Content"] = template.HTML(widgets)
	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// PasswordResetPost handles the posted password reset form.
func (controller *MainController) PasswordResetPost(c web.C, r *http.Request) (string, int) {
	email := r.FormValue("email")
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	if !controller.IsCaptchaDone(c) {
		session.AddFlash("Captcha error", "passwordresetError")
		return controller.PasswordReset(c, r)
	}

	remoteIP := getClientIP(r, controller.realIPHeader)
	user, err := helpers.EmailExists(dbMap, email)
	if err == nil {
		log.Infof("PasswordReset POST from %v, email %v", remoteIP,
			user.Email)

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

		body := "A request to reset your password was made from IP address: " +
			remoteIP + "\r\n\n" +
			"If you made this request, follow the link below:\r\n\n" +
			controller.baseURL + "/passwordupdate?t=" + token + "\r\n\n" +
			"The above link expires an hour after this email was sent.\r\n\n" +
			"If you did not make this request, you may safely ignore this " +
			"email.\r\n" + "However, you may want to look into how this " +
			"happened.\r\n"
		err := controller.SendMail(user.Email, "Voting service password reset", body)
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
	c.Env["Title"] = "Decred Voting Service - Password Update"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// PasswordUpdatePost handles updating passwords.
func (controller *MainController) PasswordUpdatePost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)
	remoteIP := getClientIP(r, controller.realIPHeader)

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
		log.Infof("UserIDExists failure %v, %v", err, remoteIP)
		session.AddFlash("Unable to find User ID", "passwordupdateError")
		return controller.PasswordUpdate(c, r)
	}

	log.Infof("PasswordUpdate POST from %v, email %v", remoteIP,
		user.Email)

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
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	user, _ := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	// Generate an API Token for the user on demand if one does not exist and
	// refresh the user's data before displaying it.
	if user.APIToken == "" {
		err := models.SetUserAPIToken(dbMap, controller.APISecret,
			controller.baseURL, user.Id)
		if err != nil {
			session.AddFlash("Unable to set API Token", "settingsError")
			log.Errorf("could not set API Token for UserId %v", user.Id)
		}

		user, _ = models.GetUserById(dbMap, session.Values["UserId"].(int64))
	}

	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["APIToken"] = user.APIToken
	c.Env["FlashError"] = session.Flashes("settingsError")
	c.Env["FlashSuccess"] = session.Flashes("settingsSuccess")
	c.Env["IsSettings"] = true
	if user.MultiSigAddress == "" {
		c.Env["ShowInstructions"] = true
	}
	if controller.smtpHost == "" {
		c.Env["SMTPDisabled"] = true
	}
	c.Env["CaptchaID"] = captcha.New()

	t := controller.GetTemplate(c)
	widgets := controller.Parse(t, "settings", c.Env)

	c.Env["Title"] = "Decred Voting Service - Settings"
	c.Env["Content"] = template.HTML(widgets)
	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// SettingsPost handles changing the user's email address or password.
func (controller *MainController) SettingsPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)
	remoteIP := getClientIP(r, controller.realIPHeader)

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

	log.Infof("Settings POST from %v, email %v", remoteIP, user.Email)

	if updateEmail == "true" {
		newEmail := r.FormValue("email")
		log.Infof("user requested email change from %v to %v", user.Email, newEmail)

		if !controller.IsCaptchaDone(c) {
			session.AddFlash("Captcha error", "settingsError")
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
			"for a voting service account at " + controller.baseURL + "\r\n" +
			"from " + user.Email + " to " + newEmail + "\r\n\n" +
			"The request was made from IP address " + remoteIP + "\r\n\n" +
			"If you made this request, follow the link below:\r\n\n" +
			controller.baseURL + "/emailupdate?t=" + token + "\r\n\n" +
			"The above link expires an hour after this email was sent.\r\n\n" +
			"If you did not make this request, you may safely ignore this " +
			"email.\r\n" + "However, you may want to look into how this " +
			"happened.\r\n"
		err = controller.SendMail(newEmail, "Voting service email change", bodyNew)
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
			"for your voting service account at " + controller.baseURL + "\r\n" +
			"from " + user.Email + " to " + newEmail + "\r\n\n" +
			"The request was made from IP address " + remoteIP + "\r\n\n" +
			"If you did not make this request, please contact the \r\n" +
			"Voting service administrator immediately.\r\n"
		err = controller.SendMail(user.Email, "Voting service email change",
			bodyOld)
		// this likely has the same status as the above email so don't
		// inform the user.
		if err != nil {
			log.Errorf("error sending email change token to old address %v %v",
				user.Email, err)
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

		// send a confirmation email.
		body := "Your voting service password for " + controller.baseURL + "\r\n" +
			"was just changed by IP Address " + remoteIP + "\r\n\n" +
			"If you did not make this request, please contact the \r\n" +
			"Voting service administrator immediately.\r\n"
		err = controller.SendMail(user.Email, "Voting service password change",
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
	remoteIP := getClientIP(r, controller.realIPHeader)

	// Validate email and password combination.
	user, err := helpers.Login(dbMap, email, password)
	if err != nil {
		log.Infof(email+" login failed %v, %v", err, remoteIP)
		session.AddFlash("Invalid Email or Password", "auth")
		return controller.SignIn(c, r)
	}

	log.Infof("SignIn POST from %v, email %v", remoteIP, user.Email)

	if user.EmailVerified == 0 {
		session.AddFlash("You must validate your email address", "auth")
		return controller.SignIn(c, r)
	}

	session.Values["UserId"] = user.Id

	// Go to Settings page if multisig script not yet set up.
	// GUI users can copy and paste their API Token from here
	// or follow the notice that directs them to the address page.
	if user.MultiSigAddress == "" {
		return "/settings", http.StatusSeeOther
	}

	// Go to Tickets page if user already set up.
	return "/tickets", http.StatusSeeOther
}

// SignUp renders the signup page.
func (controller *MainController) SignUp(c web.C, r *http.Request) (string, int) {
	// Tell main.html what route is being rendered
	c.Env["IsSignUp"] = true
	if controller.smtpHost == "" {
		c.Env["SMTPDisabled"] = true
	}
	if controller.closePool {
		c.Env["IsClosed"] = true
		c.Env["ClosePoolMsg"] = controller.closePoolMsg
	}

	session := controller.GetSession(c)
	c.Env["FlashError"] = session.Flashes("signupError")
	c.Env["FlashSuccess"] = session.Flashes("signupSuccess")
	c.Env["CaptchaID"] = captcha.New()

	t := controller.GetTemplate(c)
	widgets := controller.Parse(t, "auth/signup", c.Env)

	c.Env["Title"] = "Decred Stake Pool - Sign Up"
	c.Env["Content"] = template.HTML(widgets)
	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// SignUpPost form submit route. Registers new user or shows Sign Up route with
// appropriate messages set in session.
func (controller *MainController) SignUpPost(c web.C, r *http.Request) (string, int) {
	if controller.closePool {
		log.Infof("attempt to signup while registration disabled")
		return "/error?r=/signup", http.StatusSeeOther
	}

	session := controller.GetSession(c)
	if !controller.IsCaptchaDone(c) {
		session.AddFlash("Captcha error", "signupError")
		return controller.SignUp(c, r)
	}

	remoteIP := getClientIP(r, controller.realIPHeader)

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

	dbMap := controller.GetDbMap(c)
	user := models.GetUserByEmail(dbMap, email)

	if user != nil {
		session.AddFlash("User exists", "signupError")
		return controller.SignUp(c, r)
	}

	token := randToken()
	user = &models.User{
		Username:        email,
		Email:           email,
		EmailToken:      token,
		EmailVerified:   0,
		VoteBits:        1,
		VoteBitsVersion: int64(controller.voteVersion),
	}
	user.HashPassword(password)

	log.Infof("SignUp POST from %v, email %v. Inserting.", remoteIP, user.Email)

	if err := models.InsertUser(dbMap, user); err != nil {
		session.AddFlash("Database error occurred while adding user", "signupError")
		log.Errorf("Error while registering user: %v", err)
		return controller.SignUp(c, r)
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
	c.Env["Admin"], _ = controller.isAdmin(c, r)
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

// ByTicketHeight type implements sort.Sort for types with a TicketHeight field.
// This includes all valid tickets, including spend tickets.
type ByTicketHeight []TicketInfoLive

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

// TicketInfoLive represents live or immature (mined) tickets that have yet to
// be spent by either a vote or revocation.
type TicketInfoLive struct {
	TicketHeight uint32
	Ticket       string
}

// Tickets renders the tickets page.
func (controller *MainController) Tickets(c web.C, r *http.Request) (string, int) {

	var ticketInfoInvalid []TicketInfoInvalid
	var ticketInfoLive []TicketInfoLive
	var ticketInfoVoted, ticketInfoExpired, ticketInfoMissed []TicketInfoHistoric
	var numVoted int

	t := controller.GetTemplate(c)
	session := controller.GetSession(c)
	remoteIP := getClientIP(r, controller.realIPHeader)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	c.Env["IsTickets"] = true
	c.Env["Network"] = controller.getNetworkName()
	c.Env["PoolFees"] = controller.poolFees
	c.Env["Title"] = "Decred Stake Pool - Tickets"

	dbMap := controller.GetDbMap(c)
	user, _ := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	if user.MultiSigAddress == "" {
		log.Info("Multisigaddress empty")
		return "/address", http.StatusSeeOther
	}

	if controller.RPCIsStopped() {
		return "/error", http.StatusSeeOther
	}

	// Get P2SH Address
	multisig, err := dcrutil.DecodeAddress(user.MultiSigAddress)
	if err != nil {
		log.Warnf("Invalid address %v in database: %v", user.MultiSigAddress, err)
		return "/error", http.StatusSeeOther
	}

	log.Infof("Tickets GET from %v, multisig %v", remoteIP,
		user.MultiSigAddress)

	w := controller.rpcServers

	start := time.Now()

	spui, err := w.StakePoolUserInfo(multisig, true)
	if err != nil {
		// Render page with message to try again later
		log.Infof("RPC StakePoolUserInfo failed: %v", err)
		session.AddFlash("Unable to retrieve voting service user info", "main")
		c.Env["Flash"] = session.Flashes("main")
		return controller.Parse(t, "main", c.Env), http.StatusInternalServerError
	}

	log.Debugf(":: StakePoolUserInfo (msa = %v) execution time: %v",
		user.MultiSigAddress, time.Since(start))

	// Compute the oldest (min) ticket spend height to include in the table
	_, height, err := w.GetBestBlock()
	if err != nil {
		log.Infof("RPC GetBestBlock failed: %v", err)
		session.AddFlash("Unable to get best block height", "main")
		c.Env["Flash"] = session.Flashes("main")
		return controller.Parse(t, "main", c.Env), http.StatusInternalServerError
	}
	minVotedHeight := height - controller.maxVotedAge

	// If the user has tickets, get their info
	if spui != nil && len(spui.Tickets) > 0 {
		for _, ticket := range spui.Tickets {
			switch ticket.Status {
			case "live":
				ticketInfoLive = append(ticketInfoLive, TicketInfoLive{
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
				numVoted++
				if int64(ticket.SpentByHeight) >= minVotedHeight {
					ticketInfoVoted = append(ticketInfoVoted, TicketInfoHistoric{
						Ticket:        ticket.Ticket,
						SpentBy:       ticket.SpentBy,
						SpentByHeight: ticket.SpentByHeight,
						TicketHeight:  ticket.TicketHeight,
					})
				}
			}
		}
	}

	if spui != nil && len(spui.InvalidTickets) > 0 {
		for _, ticket := range spui.InvalidTickets {
			ticketInfoInvalid = append(ticketInfoInvalid, TicketInfoInvalid{ticket})
		}
	}

	// Sort live tickets. This is commented because the JS tables will perform
	// their own sorting anyway. However, depending on the UI implementation, it
	// may be desirable to sort it here.
	// sort.Sort(ByTicketHeight(ticketInfoLive))

	// Sort historic (voted and revoked) tickets
	sort.Sort(BySpentByHeight(ticketInfoVoted))
	sort.Sort(BySpentByHeight(ticketInfoMissed))

	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["TicketsInvalid"] = ticketInfoInvalid
	c.Env["TicketsLive"] = ticketInfoLive
	c.Env["TicketsExpired"] = ticketInfoExpired
	c.Env["TicketsMissed"] = ticketInfoMissed
	c.Env["TicketsVotedCount"] = numVoted
	c.Env["TicketsVotedArchivedCount"] = numVoted - len(ticketInfoVoted)
	c.Env["TicketsVoted"] = ticketInfoVoted
	widgets := controller.Parse(t, "tickets", c.Env)

	c.Env["Content"] = template.HTML(widgets)
	c.Env["Flash"] = session.Flashes("tickets")

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// Voting renders the voting page.
func (controller *MainController) Voting(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
	dbMap := controller.GetDbMap(c)

	if session.Values["UserId"] == nil {
		return "/", http.StatusSeeOther
	}

	user, _ := models.GetUserById(dbMap, session.Values["UserId"].(int64))

	t := controller.GetTemplate(c)

	choicesSelected := controller.choicesForAgendas(uint16(user.VoteBits))

	for k, v := range choicesSelected {
		strk := strconv.Itoa(k)
		c.Env["Agenda"+strk+"Selected"] = v
	}
	c.Env["Admin"], _ = controller.isAdmin(c, r)
	c.Env["Agendas"] = controller.getAgendas()
	c.Env["FlashError"] = session.Flashes("votingError")
	c.Env["FlashSuccess"] = session.Flashes("votingSuccess")
	c.Env["IsVoting"] = true
	c.Env["VoteVersion"] = controller.voteVersion

	widgets := controller.Parse(t, "voting", c.Env)
	c.Env["Title"] = "Decred Voting Service - Voting"
	c.Env["Content"] = template.HTML(widgets)

	return controller.Parse(t, "main", c.Env), http.StatusOK
}

// VotingPost form submit route.
func (controller *MainController) VotingPost(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)
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
		controller.StakepooldUpdateAll(dbMap, StakepooldUpdateKindUsers)
	}

	session.AddFlash("successfully updated voting preferences", "votingSuccess")
	return "/voting", http.StatusSeeOther
}

// Logout the user.
func (controller *MainController) Logout(c web.C, r *http.Request) (string, int) {
	session := controller.GetSession(c)

	session.Values["UserId"] = nil

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
	if controller.params.Deployments == nil {
		return nil
	}

	return controller.params.Deployments[controller.voteVersion]

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
	minutesUntilExpiryEstimate := time.Duration(controller.params.TicketExpiry) * controller.params.TargetTimePerBlock
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
