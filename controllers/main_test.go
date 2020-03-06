package controllers

import (
	"database/sql/driver"
	mrand "math/rand"
	"net/http"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrstakepool/models"
	"github.com/decred/dcrstakepool/poolapi"
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
	tUserFeeAddr := "TsbyH2p611jSWnvUAq3erSsRYnCxBg3nT2S"
	tests := []struct {
		name             string
		userID           int64
		queryArgs        []driver.Value
		row              []driver.Value
		wantErrCode      codes.Code
		wantPurchaseInfo *poolapi.PurchaseInfo
	}{{
		name:        "ok",
		userID:      1,
		queryArgs:   []driver.Value{1},
		row:         []driver.Value{1, "", "", "", tMSA, tRedeemScript, "", "non null", tUserFeeAddr, 0, 0, "", "", 5, 0},
		wantErrCode: codes.OK,
		wantPurchaseInfo: &poolapi.PurchaseInfo{
			PoolAddress:   tUserFeeAddr,
			PoolFees:      0.15,
			Script:        tRedeemScript,
			TicketAddress: tMSA,
			VoteBits:      5,
		},
	}, {
		name:        "not authenticated",
		userID:      -1,
		wantErrCode: codes.Unauthenticated,
	}, {
		name:        "error retrieving from database",
		userID:      1,
		wantErrCode: codes.Internal,
	}, {
		name:        "user address not submitted",
		userID:      1,
		queryArgs:   []driver.Value{1},
		row:         []driver.Value{1, "", "", "", "", "", "", "", "", 0, 0, "", "", 0, 0},
		wantErrCode: codes.FailedPrecondition,
	}}
	c := web.C{Env: map[interface{}]interface{}{}}
	for _, test := range tests {
		mock, dbMap := makeDB()
		cfg := &Config{PoolFees: 0.15}
		mc := &MainController{Cfg: cfg}
		// Set expected database queries.
		if test.queryArgs != nil {
			mock.ExpectQuery(`^SELECT (.*) FROM Users WHERE UserId = (.+)$`).
				WithArgs(test.queryArgs...).
				WillReturnRows(sqlmock.NewRows(tUserCol).AddRow(test.row...))
		}
		c.Env["APIUserID"] = test.userID
		// nil APIUserID indicats an authentication error occured.
		// Simulating that on userid -1.
		if test.userID == -1 {
			c.Env["APIUserID"] = nil
		}
		c.Env["DbMap"] = dbMap
		r, _ := http.NewRequest("GET", "", nil)
		pi, errCode, _, _ := mc.APIPurchaseInfo(c, r)
		if errCode != test.wantErrCode {
			t.Fatalf("wanted error code %d but got %d for test %s", test.wantErrCode, errCode, test.name)
		}
		// Ensure all database commands were called.
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectation error: %s", err)
		}
		if !reflect.DeepEqual(pi, test.wantPurchaseInfo) {
			t.Fatalf("wanted and got purchase info not equal for test %s", test.name)
		}
	}
}
