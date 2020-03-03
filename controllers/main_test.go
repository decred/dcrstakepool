package controllers

import (
	"database/sql/driver"
	"errors"
	mrand "math/rand"
	"net/http"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	pb "github.com/decred/dcrstakepool/backend/stakepoold/rpc/stakepoolrpc"
	"github.com/decred/dcrstakepool/models"
	"github.com/decred/dcrstakepool/stakepooldclient"
	"github.com/decred/slog"
	"github.com/go-gorp/gorp"
	"github.com/zenazn/goji/web"
	"google.golang.org/grpc/codes"
)

func init() {
	// Enable logging for the controllers package.
	log = slog.NewBackend(os.Stdout).Logger("TEST")
	log.SetLevel(slog.LevelTrace)
}

func TestGetNetworkName(t *testing.T) {
	// First test that "testnet3" is translated to "testnet"
	cfg := Config{
		NetParams: chaincfg.TestNet3Params(),
	}

	mc := MainController{
		Cfg: &cfg,
	}

	netName := mc.getNetworkName()
	if netName != "testnet" {
		t.Errorf("Incorrect network name: expected %s, got %s", "testnet",
			netName)
	}

	// ensure "mainnet" is unaltered
	mc.Cfg.NetParams = chaincfg.MainNetParams()
	netName = mc.getNetworkName()
	if netName != "mainnet" {
		t.Errorf("Incorrect network name: expected %s, got %s", "mainnet",
			netName)
	}
}

func randHashString() string {
	var b [64]byte
	const hexvals = "123456789abcdef"
	for i := range b {
		b[i] = hexvals[mrand.Intn(len(hexvals))]
	}
	return string(b[:])
}

func TestSortByTicketHeight(t *testing.T) {
	// Create a large list of tickets to sort, voted over many blocks
	ticketCount, maxTxHeight := 55000, int64(123000)

	ticketInfoLive := make([]TicketInfo, 0, ticketCount)
	for i := 0; i < ticketCount; i++ {
		ticketInfoLive = append(ticketInfoLive, TicketInfo{
			TicketHeight: uint32(mrand.Int63n(maxTxHeight)),
			Ticket:       randHashString(), // could be nothing unless we sort with it
		})
	}

	// Make a copy to sort with ref method
	ticketInfoLive2 := make([]TicketInfo, len(ticketInfoLive))
	copy(ticketInfoLive2, ticketInfoLive)

	// Sort with ByTicketHeight, the test subject
	sort.Sort(ByTicketHeight(ticketInfoLive))

	// Sort using convenience function added in go1.8
	sort.Slice(ticketInfoLive2, func(i, j int) bool {
		return ticketInfoLive2[i].TicketHeight < ticketInfoLive2[j].TicketHeight
	})
	// compare
	if !reflect.DeepEqual(ticketInfoLive, ticketInfoLive2) {
		t.Error("Sort with ByTicketHeight failed")
	}

	// Check if sorted using convenience function added in go1.8
	if !sort.SliceIsSorted(ticketInfoLive, func(i, j int) bool {
		return ticketInfoLive[i].TicketHeight < ticketInfoLive[j].TicketHeight
	}) {
		t.Error("Sort with ByTicketHeight failed")
	}
}

func BenchmarkSortByTicketHeight100(b *testing.B)   { benchmarkSortByTicketHeight(100, b) }
func BenchmarkSortByTicketHeight500(b *testing.B)   { benchmarkSortByTicketHeight(500, b) }
func BenchmarkSortByTicketHeight1000(b *testing.B)  { benchmarkSortByTicketHeight(1000, b) }
func BenchmarkSortByTicketHeight2500(b *testing.B)  { benchmarkSortByTicketHeight(2500, b) }
func BenchmarkSortByTicketHeight5000(b *testing.B)  { benchmarkSortByTicketHeight(5000, b) }
func BenchmarkSortByTicketHeight10000(b *testing.B) { benchmarkSortByTicketHeight(10000, b) }
func BenchmarkSortByTicketHeight20000(b *testing.B) { benchmarkSortByTicketHeight(20000, b) }

func benchmarkSortByTicketHeight(ticketCount int, b *testing.B) {
	// Create a large list of tickets to sort, voted over many blocks
	maxTxHeight := int64(53000)

	ticketInfoLive := make([]TicketInfo, 0, ticketCount)
	for i := 0; i < ticketCount; i++ {
		ticketInfoLive = append(ticketInfoLive, TicketInfo{
			TicketHeight: uint32(mrand.Int63n(maxTxHeight)),
			Ticket:       randHashString(), // could be nothing unless we sort with it
		})
	}

	for i := 0; i < b.N; i++ {
		// Make a copy to sort
		ticketInfoLive2 := make([]TicketInfo, len(ticketInfoLive))
		copy(ticketInfoLive2, ticketInfoLive)

		// Sort with ByTicketHeight, the test subject
		sort.Sort(ByTicketHeight(ticketInfoLive2))
	}
}

func BenchmarkSortBySpentByHeight100(b *testing.B)   { benchmarkSortBySpentByHeight(100, b) }
func BenchmarkSortBySpentByHeight500(b *testing.B)   { benchmarkSortBySpentByHeight(500, b) }
func BenchmarkSortBySpentByHeight1000(b *testing.B)  { benchmarkSortBySpentByHeight(1000, b) }
func BenchmarkSortBySpentByHeight2500(b *testing.B)  { benchmarkSortBySpentByHeight(2500, b) }
func BenchmarkSortBySpentByHeight5000(b *testing.B)  { benchmarkSortBySpentByHeight(5000, b) }
func BenchmarkSortBySpentByHeight10000(b *testing.B) { benchmarkSortBySpentByHeight(10000, b) }
func BenchmarkSortBySpentByHeight20000(b *testing.B) { benchmarkSortBySpentByHeight(20000, b) }

func benchmarkSortBySpentByHeight(ticketCount int, b *testing.B) {
	// Create a large list of tickets to sort, voted over many blocks
	maxTxHeight := int64(53000)

	ticketInfoVoted := make([]TicketInfoHistoric, 0, ticketCount)
	for i := 0; i < ticketCount; i++ {
		ticketInfoVoted = append(ticketInfoVoted, TicketInfoHistoric{
			Ticket:        randHashString(), // could be nothing unless we sort with it
			SpentBy:       randHashString(),
			SpentByHeight: uint32(mrand.Int63n(maxTxHeight)),
			TicketHeight:  uint32(mrand.Int63n(maxTxHeight)),
		})
	}

	for i := 0; i < b.N; i++ {
		// Make a copy to sort
		ticketInfoVoted2 := make([]TicketInfoHistoric, len(ticketInfoVoted))
		copy(ticketInfoVoted2, ticketInfoVoted)

		// Sort with BySpentByHeight, the test subject
		sort.Sort(BySpentByHeight(ticketInfoVoted2))
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name, realIPHeader, realAddr, remoteAddr, wantAddr string
	}{{
		name:         "has realIPHeader default name",
		realIPHeader: "X-Real-IP",
		realAddr:     "240.111.3.145:3000",
		wantAddr:     "240.111.3.145",
	}, {
		name:         "has realIPHeader no port",
		realIPHeader: "X-Real-IP",
		realAddr:     "240.111.3.145",
		wantAddr:     "240.111.3.145",
	}, {
		name:         "has realIPHeader custom name",
		realIPHeader: "the real ip",
		realAddr:     "240.111.3.145:5454",
		wantAddr:     "240.111.3.145",
	}, {
		name:         "has realIPHeader host name",
		realIPHeader: "X-Real-IP",
		realAddr:     "hosting service",
		wantAddr:     "hosting service",
	}, {
		name:       "no realIPHeader has remoteAddr",
		remoteAddr: "240.111.3.145:80",
		wantAddr:   "240.111.3.145",
	}, {
		name:       "no realIPHeader has remoteAddr no port",
		remoteAddr: "240.111.3.145",
		wantAddr:   "240.111.3.145",
	}, {
		name:       "no realIPHeader has remoteAddr host name",
		remoteAddr: "hosting service",
		wantAddr:   "hosting service",
	}, {
		name:     "no realIPHeader no remoteAddr",
		wantAddr: "",
	}}

	r, _ := http.NewRequest("GET", "", nil)
	for _, test := range tests {
		requestHeader := make(http.Header)
		if test.realIPHeader != "" {
			requestHeader.Add(test.realIPHeader, test.realAddr)
		}
		r.RemoteAddr = test.remoteAddr
		r.Header = requestHeader
		addr := getClientIP(r, test.realIPHeader)
		if addr != test.wantAddr {
			t.Fatalf("expected \"%v\" for \"%v\" but got \"%v\"", test.wantAddr, test.name, addr)
		}
	}
}

const (
	voteIDFixLNSeqLocks     = "fixlnseqlocks"
	voteIDHeaderCommitments = "headercommitments"
)

var tDeployments = map[uint32][]chaincfg.ConsensusDeployment{
	7: {{
		Vote: chaincfg.Vote{
			Id:          voteIDFixLNSeqLocks,
			Description: "Modify sequence lock handling as defined in DCP0004",
			Mask:        0x0006, // Bits 1 and 2
			Choices: []chaincfg.Choice{{
				Id:          "abstain",
				Description: "abstain voting for change",
				Bits:        0x0000,
				IsAbstain:   true,
				IsNo:        false,
			}, {
				Id:          "no",
				Description: "keep the existing consensus rules",
				Bits:        0x0002, // Bit 1
				IsAbstain:   false,
				IsNo:        true,
			}, {
				Id:          "yes",
				Description: "change to the new consensus rules",
				Bits:        0x0004, // Bit 2
				IsAbstain:   false,
				IsNo:        false,
			}},
		},
		StartTime:  1548633600, // Jan 28th, 2019
		ExpireTime: 1580169600, // Jan 28th, 2020
	}},
	8: {{
		Vote: chaincfg.Vote{
			Id:          voteIDHeaderCommitments,
			Description: "Enable header commitments as defined in DCP0005",
			Mask:        0x0006, // Bits 1 and 2
			Choices: []chaincfg.Choice{{
				Id:          "abstain",
				Description: "abstain voting for change",
				Bits:        0x0000,
				IsAbstain:   true,
				IsNo:        false,
			}, {
				Id:          "no",
				Description: "keep the existing consensus rules",
				Bits:        0x0002, // Bit 1
				IsAbstain:   false,
				IsNo:        true,
			}, {
				Id:          "yes",
				Description: "change to the new consensus rules",
				Bits:        0x0004, // Bit 2
				IsAbstain:   false,
				IsNo:        false,
			}},
		},
		StartTime:  1567641600, // Sep 5th, 2019
		ExpireTime: 1599264000, // Sep 5th, 2020
	}},
}

func TestGetAgendas(t *testing.T) {
	tests := []struct {
		name        string
		voteVersion uint32
		deployments map[uint32][]chaincfg.ConsensusDeployment
		want        []chaincfg.ConsensusDeployment
	}{{
		name:        "ok",
		voteVersion: 7,
		deployments: tDeployments,
		want:        tDeployments[7],
	}, {
		name:        "nonexistant deployment",
		voteVersion: 2,
		deployments: tDeployments,
		want:        nil,
	}, {
		name:        "no deployments",
		voteVersion: 7,
		want:        nil,
	}}
	for _, test := range tests {
		params := &chaincfg.Params{Deployments: test.deployments}
		cfg := &Config{NetParams: params}
		mc := &MainController{Cfg: cfg, voteVersion: test.voteVersion}
		agendas := mc.getAgendas()
		if !reflect.DeepEqual(agendas, test.want) {
			t.Fatalf("expected deployments %v for test %s but got %v", test.want, test.name, agendas)
		}
	}
}

var _ stakepooldclient.Manager = (*tStakepooldManager)(nil)

// tStakepooldManager satisfies the stakepooldClient.Manager interface.
type tStakepooldManager struct {
	qItem func() queueItem
}

func (m *tStakepooldManager) GetAddedLowFeeTickets() (map[chainhash.Hash]string, error) {
	item := m.qItem()
	thing, _ := item.thing.(map[chainhash.Hash]string)
	return thing, item.err
}
func (m *tStakepooldManager) GetIgnoredLowFeeTickets() (map[chainhash.Hash]string, error) {
	item := m.qItem()
	thing, _ := item.thing.(map[chainhash.Hash]string)
	return thing, item.err
}
func (m *tStakepooldManager) GetLiveTickets() (map[chainhash.Hash]string, error) {
	item := m.qItem()
	thing, _ := item.thing.(map[chainhash.Hash]string)
	return thing, item.err
}
func (m *tStakepooldManager) SetAddedLowFeeTickets(_ []models.LowFeeTicket) error {
	item := m.qItem()
	return item.err
}
func (m *tStakepooldManager) CreateMultisig(_ []string) (*pb.CreateMultisigResponse, error) {
	item := m.qItem()
	thing, _ := item.thing.(*pb.CreateMultisigResponse)
	return thing, item.err
}
func (m *tStakepooldManager) SyncAll(_ []models.User, _ int64) error {
	item := m.qItem()
	return item.err
}
func (m *tStakepooldManager) StakePoolUserInfo(_ string) (*pb.StakePoolUserInfoResponse, error) {
	item := m.qItem()
	thing, _ := item.thing.(*pb.StakePoolUserInfoResponse)
	return thing, item.err
}
func (m *tStakepooldManager) SetUserVotingPrefs(_ map[int64]*models.User) error {
	item := m.qItem()
	return item.err
}
func (m *tStakepooldManager) WalletInfo() ([]*pb.WalletInfoResponse, error) {
	item := m.qItem()
	thing, _ := item.thing.([]*pb.WalletInfoResponse)
	return thing, item.err
}
func (m *tStakepooldManager) ValidateAddress(_ dcrutil.Address) (*pb.ValidateAddressResponse, error) {
	item := m.qItem()
	thing, _ := item.thing.(*pb.ValidateAddressResponse)
	return thing, item.err
}
func (m *tStakepooldManager) ImportNewScript(_ []byte) (heightImported int64, err error) {
	item := m.qItem()
	thing, _ := item.thing.(int64)
	return thing, item.err
}
func (m *tStakepooldManager) BackendStatus() []stakepooldclient.BackendStatus {
	item := m.qItem()
	thing, _ := item.thing.([]stakepooldclient.BackendStatus)
	return thing
}
func (m *tStakepooldManager) GetStakeInfo() (*pb.GetStakeInfoResponse, error) {
	item := m.qItem()
	thing, _ := item.thing.(*pb.GetStakeInfoResponse)
	return thing, item.err
}
func (m *tStakepooldManager) CrossCheckColdWalletExtPubs(_ string) error {
	item := m.qItem()
	return item.err
}

type queueItem struct {
	thing interface{}
	err   error
}

// tManagerWithQueue will return a tStakepooldManager that outputs items in the
// order of queueItems.
func tManagerWithQueue(queueItems []queueItem) *tStakepooldManager {
	i := 0
	getItem := func() queueItem {
		defer func() { i++ }()
		return queueItems[i]
	}
	sm := &tStakepooldManager{getItem}
	return sm
}

func TestNewMainController(t *testing.T) {
	tests := []struct {
		name            string
		stakepooldQueue []queueItem
		wantErr         bool
	}{{
		name: "ok one wallet",
		stakepooldQueue: []queueItem{{
			thing: []*pb.WalletInfoResponse{{
				VoteVersion: 7,
			}},
		}},
		wantErr: false,
	}, {
		name: "ok two wallets",
		stakepooldQueue: []queueItem{{
			thing: []*pb.WalletInfoResponse{{
				VoteVersion: 7,
			}, {
				VoteVersion: 7,
			}},
		}},
		wantErr: false,
	}, {
		name: "wallet different vote version",
		stakepooldQueue: []queueItem{{
			thing: []*pb.WalletInfoResponse{{
				VoteVersion: 5,
			}, {
				VoteVersion: 7,
			}},
		}},
		wantErr: true,
	}, {
		name: "wallet info error",
		stakepooldQueue: []queueItem{{
			err: errors.New("error"),
		}},
		wantErr: true,
	}}
	for _, test := range tests {
		sm := tManagerWithQueue(test.stakepooldQueue)
		cfg := &Config{StakepooldServers: sm, NetParams: chaincfg.TestNet3Params()}
		_, err := NewMainController(cfg)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test \"%s\"", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test \"%s\": %v", test.name, err)
		}
	}
}

// makeDB creates a fake database for testing.
func makeDB() (sqlmock.Sqlmock, *gorp.DbMap) {
	db, mock, err := sqlmock.New()
	if err != nil {
		panic(err)
	}
	dbMap := &gorp.DbMap{
		Db:              db,
		Dialect:         gorp.MySQLDialect{Engine: "InnoDB", Encoding: "UTF8MB4"},
		ExpandSliceArgs: true,
	}
	dbMap.AddTableWithName(models.User{}, "Users").SetKeys(true, "ID")
	return mock, dbMap
}

var tUserCol = []string{"UserId", "Email", "Username", "Password",
	"MultiSigAddress", "MultiSigScript", "PoolPubKeyAddr",
	"UserPubKeyAddr", "UserFeeAddr", "HeightRegistered",
	"EmailVerified", "EmailToken", "APIToken", "VoteBits", "VoteBitsVersion"}

func TestAPIAddress(t *testing.T) {
	tMSA := "Tcbvn2hiEAXBDwUPDLDG2SxF9iANMKhdVev"
	tRedeemScript := "512103af3c24d005ca8b755e7167617f3a5b4c60a65f8" +
		"318a7fcd1b0cacb1abd2a97fc21027b81bc16954e28adb83224814" +
		"0eb58bedb6078ae5f4dabf21fde5a8ab7135cb652ae"
	tUserPubKeyAddr := "TkKmVKG7u7PwhQaYr7wgMqBwHneJ2cN4e5YpMVUsWSopx81NFXEzK"
	tPoolPubKeyAddr := "TkQ4hPAjU7fWxNgTdNxDXzrGQc1r4w2yjeM667Rkxa6MtsgcJmNpu"
	tUserFeeAddr := "TsbyH2p611jSWnvUAq3erSsRYnCxBg3nT2S"
	tVotingXpub := "tpubVpoboFfxtp3JShb4cvMNpeTFp48tYx8cSAJBphAE4iR" +
		"q1BqzPQBZ912mLDqh9Z4TnrzgnCMDt93A9qgpfoBAX4VxbeRY1tasnNNkBZZR6vU"
	tFeeXpub := "tpubVpCySS6qPiiHrLHzkL2kdbZrW8ftwax4frjXp12mUMDmho" +
		"pdhfuG2JAW8a8Z22at4uqGiwFenEzY8uVhJ3nGsfVFTFtKbnkEPuxYsp3rZYb"
	tUser := []driver.Value{46, "", "", "", "", "", "", "", "", 0, 0, "", "", 0, 0}
	params := chaincfg.TestNet3Params()
	vXpub, err := hdkeychain.NewKeyFromString(tVotingXpub, params)
	if err != nil {
		t.Fatal(err)
	}
	fXpub, err := hdkeychain.NewKeyFromString(tFeeXpub, params)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, userPubKeyAddr string
		userID               int64
		stakepooldQueue      []queueItem
		queryArgs            []driver.Value
		noSecondQuery        bool
		updateArgs           []driver.Value
		row                  []driver.Value
		wantErrCode          codes.Code
	}{{
		name:           "ok",
		userID:         46,
		userPubKeyAddr: tUserPubKeyAddr,
		stakepooldQueue: []queueItem{{
			// ValidateAddress
			thing: &pb.ValidateAddressResponse{
				IsMine:     true,
				PubKeyAddr: tPoolPubKeyAddr,
			}}, {
			// CreateMultisig
			thing: &pb.CreateMultisigResponse{
				RedeemScript: tRedeemScript,
				Address:      tMSA,
			}}, {
			// ImportNewScript
			thing: 0,
		}, {
			// SetUserVotingPrefs
		}},
		queryArgs:   []driver.Value{46},
		updateArgs:  []driver.Value{"", "", []byte{}, tMSA, tRedeemScript, tPoolPubKeyAddr, tUserPubKeyAddr, tUserFeeAddr, 0, 0, "", "", 0, 0, 46},
		row:         tUser,
		wantErrCode: codes.OK,
	}, {
		name:        "invalid api token",
		userID:      -1,
		wantErrCode: codes.Unauthenticated,
	}, {
		name:          "user pubkey address already exists",
		userID:        46,
		queryArgs:     []driver.Value{46},
		noSecondQuery: true,
		row:           append(append(append([]driver.Value{}, tUser[:7]...), "pubkeyaddr"), tUser[8:]...),
		wantErrCode:   codes.AlreadyExists,
	}, {
		name:           "bad user pubkey address",
		userID:         46,
		userPubKeyAddr: "bogus address",
		queryArgs:      []driver.Value{46},
		row:            tUser,
		noSecondQuery:  true,
		wantErrCode:    codes.InvalidArgument,
	}, {
		name:           "over max users",
		userID:         MaxUsers + 1,
		userPubKeyAddr: tUserPubKeyAddr,
		queryArgs:      []driver.Value{MaxUsers + 1},
		row:            tUser,
		noSecondQuery:  true,
		wantErrCode:    codes.Unavailable,
	}, {
		name:           "unable to validate pool address",
		userID:         46,
		userPubKeyAddr: tUserPubKeyAddr,
		stakepooldQueue: []queueItem{{
			// ValidateAddress
			err: errors.New("error"),
		}},
		queryArgs:     []driver.Value{46},
		row:           tUser,
		noSecondQuery: true,
		wantErrCode:   codes.Unavailable,
	}, {
		name:           "pool address is not mine",
		userID:         46,
		userPubKeyAddr: tUserPubKeyAddr,
		stakepooldQueue: []queueItem{{
			// ValidateAddress
			thing: &pb.ValidateAddressResponse{
				IsMine: false,
			}}},
		queryArgs:     []driver.Value{46},
		row:           tUser,
		noSecondQuery: true,
		wantErrCode:   codes.Unavailable,
	}, {
		name:           "unable to decode pool pubkey address",
		userID:         46,
		userPubKeyAddr: tUserPubKeyAddr,
		stakepooldQueue: []queueItem{{
			// ValidateAddress
			thing: &pb.ValidateAddressResponse{
				IsMine:     true,
				PubKeyAddr: "bogus address",
			}}},
		queryArgs:     []driver.Value{46},
		row:           tUser,
		noSecondQuery: true,
		wantErrCode:   codes.Unavailable,
	}, {
		name:           "unable to create multisig",
		userID:         46,
		userPubKeyAddr: tUserPubKeyAddr,
		stakepooldQueue: []queueItem{{
			// ValidateAddress
			thing: &pb.ValidateAddressResponse{
				IsMine:     true,
				PubKeyAddr: tPoolPubKeyAddr,
			}}, {
			// CreateMultisig
			err: errors.New("error"),
		}},
		queryArgs:     []driver.Value{46},
		row:           tUser,
		noSecondQuery: true,
		wantErrCode:   codes.Unavailable,
	}, {
		name:           "unable to serialize redeem script",
		userID:         46,
		userPubKeyAddr: tUserPubKeyAddr,
		stakepooldQueue: []queueItem{{
			// ValidateAddress
			thing: &pb.ValidateAddressResponse{
				IsMine:     true,
				PubKeyAddr: tPoolPubKeyAddr,
			}}, {
			// CreateMultisig
			thing: &pb.CreateMultisigResponse{
				RedeemScript: "bogus script",
				Address:      tMSA,
			}}},
		queryArgs:     []driver.Value{46},
		row:           tUser,
		noSecondQuery: true,
		wantErrCode:   codes.Unavailable,
	}, {
		name:           "unable to import redeem script",
		userID:         46,
		userPubKeyAddr: tUserPubKeyAddr,
		stakepooldQueue: []queueItem{{
			// ValidateAddress
			thing: &pb.ValidateAddressResponse{
				IsMine:     true,
				PubKeyAddr: tPoolPubKeyAddr,
			}}, {
			// CreateMultisig
			thing: &pb.CreateMultisigResponse{
				RedeemScript: tRedeemScript,
				Address:      tMSA,
			}}, {
			// ImportNewScript
			err: errors.New("error"),
		}},
		queryArgs:     []driver.Value{46},
		row:           tUser,
		noSecondQuery: true,
		wantErrCode:   codes.Unavailable,
	}}
	c := web.C{Env: map[interface{}]interface{}{}}
	for _, test := range tests {
		mock, dbMap := makeDB()
		sm := tManagerWithQueue(test.stakepooldQueue)
		cfg := &Config{StakepooldServers: sm, NetParams: params, VotingXpub: vXpub, FeeXpub: fXpub}
		mc := &MainController{Cfg: cfg}
		// Set expected database queries and updates.
		if test.queryArgs != nil {
			mock.ExpectQuery(`^SELECT (.*) FROM Users WHERE UserId = (.+)$`).
				WithArgs(test.queryArgs...).
				WillReturnRows(sqlmock.NewRows(tUserCol).AddRow(test.row...))
			if !test.noSecondQuery {
				mock.ExpectQuery(`^SELECT (.*) FROM Users WHERE UserId = (.+)$`).
					WithArgs(test.queryArgs...).
					WillReturnRows(sqlmock.NewRows(tUserCol).AddRow(test.row...))
			}
		}
		if test.updateArgs != nil {
			mock.ExpectExec("^update `Users` set `Email`=(.+)," +
				" `Username`=(.+), `Password`=(.+), `MultiSigAddress`=(.+), " +
				"`MultiSigScript`=(.+), `PoolPubKeyAddr`=(.+), `UserPubKeyAddr`=(.+), " +
				"`UserFeeAddr`=(.+), `HeightRegistered`=(.+), `EmailVerified`=(.+), " +
				"`EmailToken`=(.+), `APIToken`=(.+), `VoteBits`=(.+), " +
				"`VoteBitsVersion`=(.+) where `UserId`=(.+);$").
				WithArgs(test.updateArgs...).
				WillReturnResult(sqlmock.NewResult(0, 0))
		}
		c.Env["APIUserID"] = test.userID
		// nil APIUserID indicats an authentication error occured.
		// Simulating that on userid -1.
		if test.userID == -1 {
			c.Env["APIUserID"] = nil
		}
		c.Env["DbMap"] = dbMap
		// Put userPubKeyAddr as form value.
		r, _ := http.NewRequest("GET", "?UserPubKeyAddr="+test.userPubKeyAddr, nil)
		_, errCode, _, _ := mc.APIAddress(c, r)
		if errCode != test.wantErrCode {
			t.Fatalf("wanted error code %d but got %d for test %s", test.wantErrCode, errCode, test.name)
		}
		// Ensure all database commands were called.
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectation error: %s", err)
		}
	}
}
