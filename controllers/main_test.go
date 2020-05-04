package controllers

import (
	"context"
	"encoding/json"
	"errors"
	mrand "math/rand"
	"net"
	"net/http"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	dcrdatatypes "github.com/decred/dcrdata/api/types/v5"
	"github.com/decred/dcrdata/db/dbtypes/v2"
	pb "github.com/decred/dcrstakepool/backend/stakepoold/rpc/stakepoolrpc"
	"github.com/decred/dcrstakepool/models"
	"github.com/decred/dcrstakepool/stakepooldclient"
	"github.com/decred/slog"
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

var _ stakepooldclient.Manager = (*tStakepooldManager)(nil)

// tStakepooldManager satisfies the stakepooldClient.Manager interface.
type tStakepooldManager struct {
	qItem func() queueItem
}

func (m *tStakepooldManager) GetAddedLowFeeTickets(_ context.Context) (map[chainhash.Hash]string, error) {
	item := m.qItem()
	thing, _ := item.thing.(map[chainhash.Hash]string)
	return thing, item.err
}
func (m *tStakepooldManager) GetIgnoredLowFeeTickets(_ context.Context) (map[chainhash.Hash]string, error) {
	item := m.qItem()
	thing, _ := item.thing.(map[chainhash.Hash]string)
	return thing, item.err
}
func (m *tStakepooldManager) GetLiveTickets(_ context.Context) (map[chainhash.Hash]string, error) {
	item := m.qItem()
	thing, _ := item.thing.(map[chainhash.Hash]string)
	return thing, item.err
}
func (m *tStakepooldManager) SetAddedLowFeeTickets(_ context.Context, _ []models.LowFeeTicket) error {
	item := m.qItem()
	return item.err
}
func (m *tStakepooldManager) CreateMultisig(_ context.Context, _ []string) (*pb.CreateMultisigResponse, error) {
	item := m.qItem()
	thing, _ := item.thing.(*pb.CreateMultisigResponse)
	return thing, item.err
}
func (m *tStakepooldManager) SyncAll(_ context.Context, _ []models.User, _ int64) error {
	item := m.qItem()
	return item.err
}
func (m *tStakepooldManager) StakePoolUserInfo(_ context.Context, _ string) (*pb.StakePoolUserInfoResponse, error) {
	item := m.qItem()
	thing, _ := item.thing.(*pb.StakePoolUserInfoResponse)
	return thing, item.err
}
func (m *tStakepooldManager) SetUserVotingPrefs(_ context.Context, _ map[int64]*models.User) error {
	item := m.qItem()
	return item.err
}
func (m *tStakepooldManager) WalletInfo(_ context.Context) ([]*pb.WalletInfoResponse, error) {
	item := m.qItem()
	thing, _ := item.thing.([]*pb.WalletInfoResponse)
	return thing, item.err
}
func (m *tStakepooldManager) ValidateAddress(_ context.Context, _ dcrutil.Address) (*pb.ValidateAddressResponse, error) {
	item := m.qItem()
	thing, _ := item.thing.(*pb.ValidateAddressResponse)
	return thing, item.err
}
func (m *tStakepooldManager) ImportNewScript(_ context.Context, _ []byte) (heightImported int64, err error) {
	item := m.qItem()
	thing, _ := item.thing.(int64)
	return thing, item.err
}
func (m *tStakepooldManager) BackendStatus(_ context.Context) []stakepooldclient.BackendStatus {
	item := m.qItem()
	thing, _ := item.thing.([]stakepooldclient.BackendStatus)
	return thing
}
func (m *tStakepooldManager) GetStakeInfo(_ context.Context) (*pb.GetStakeInfoResponse, error) {
	item := m.qItem()
	thing, _ := item.thing.(*pb.GetStakeInfoResponse)
	return thing, item.err
}
func (m *tStakepooldManager) CrossCheckColdWalletExtPubs(_ context.Context, _ string) error {
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

	ctx := context.Background()
	for _, test := range tests {
		sm := tManagerWithQueue(test.stakepooldQueue)
		cfg := &Config{StakepooldServers: sm, NetParams: chaincfg.TestNet3Params()}
		_, err := NewMainController(ctx, cfg)
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

const (
	voteIDSDiffAlgorithm    = "diffalgorithm"
	voteIDLNSupport         = "lnsupport"
	voteIDFixLNSeqLocks     = "fixlnseqlocks"
	voteIDHeaderCommitments = "headercommitments"
)

var tDeployments = map[uint32][]chaincfg.ConsensusDeployment{
	4: {{
		Vote: chaincfg.Vote{
			Id:          voteIDSDiffAlgorithm,
			Description: "Change stake difficulty algorithm as defined in DCP0001",
			Mask:        0x0006, // Bits 1 and 2
			Choices: []chaincfg.Choice{{
				Id:          "abstain",
				Description: "abstain voting for change",
				Bits:        0x0000,
				IsAbstain:   true,
				IsNo:        false,
			}, {
				Id:          "no",
				Description: "keep the existing algorithm",
				Bits:        0x0002, // Bit 1
				IsAbstain:   false,
				IsNo:        true,
			}, {
				Id:          "yes",
				Description: "change to the new algorithm",
				Bits:        0x0004, // Bit 2
				IsAbstain:   false,
				IsNo:        false,
			}},
		},
		StartTime:  1493164800, // Apr 26th, 2017
		ExpireTime: 1524700800, // Apr 26th, 2018
	}, {
		Vote: chaincfg.Vote{
			Id:          voteIDLNSupport,
			Description: "Request developers begin work on Lightning Network (LN) integration",
			Mask:        0x0018, // Bits 3 and 4
			Choices: []chaincfg.Choice{{
				Id:          "abstain",
				Description: "abstain from voting",
				Bits:        0x0000,
				IsAbstain:   true,
				IsNo:        false,
			}, {
				Id:          "no",
				Description: "no, do not work on integrating LN support",
				Bits:        0x0008, // Bit 3
				IsAbstain:   false,
				IsNo:        true,
			}, {
				Id:          "yes",
				Description: "yes, begin work on integrating LN support",
				Bits:        0x0010, // Bit 4
				IsAbstain:   false,
				IsNo:        false,
			}},
		},
		StartTime:  1493164800, // Apr 26th, 2017
		ExpireTime: 1508976000, // Oct 26th, 2017
	}},
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

// tServe serves up thing at address addr. Returns a function that must be
// called to release resources.
func tServe(addr string, thing interface{}) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	handleJSON := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		encoder := json.NewEncoder(w)
		if err := encoder.Encode(thing); err != nil {
			panic(err)
		}
	}
	srv := &http.Server{Handler: http.HandlerFunc(handleJSON)}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}
	go func() {
		<-ctx.Done()
		if err := srv.Shutdown(context.Background()); err != nil {
			panic(err)
		}
	}()
	go func() {
		if err := srv.Serve(listener); err != http.ErrServerClosed {
			panic(err)
		}
		close(done)
	}()
	return func() {
		cancel()
		<-done
	}
}

func TestAgendas(t *testing.T) {
	tInfos := []*dcrdatatypes.AgendasInfo{{
		Name:      voteIDSDiffAlgorithm,
		MileStone: &dbtypes.MileStone{Status: dbtypes.FailedAgendaStatus},
	}, {
		Name:      voteIDLNSupport,
		MileStone: &dbtypes.MileStone{Status: dbtypes.ActivatedAgendaStatus},
	}}
	tAgendas := &[]agenda{{
		Agenda: tDeployments[4][0],
		Status: "failed",
	}, {
		Agenda: tDeployments[4][1],
		Status: "finished",
	}}
	tests := []struct {
		name           string
		infos          []*dcrdatatypes.AgendasInfo
		agendasInitial *[]agenda
		timerInitial   time.Time
		deployments    map[uint32][]chaincfg.ConsensusDeployment
		want           *[]agenda
	}{{
		name:        "no error or initial agendas",
		infos:       tInfos,
		deployments: tDeployments,
		want:        tAgendas,
	}, {
		name: "no error with expired initial agendas",
		agendasInitial: &[]agenda{{
			Agenda: tDeployments[7][0],
			Status: "everyone voted no",
		}},
		infos:       tInfos,
		deployments: tDeployments,
		want:        tAgendas,
	}, {
		name:           "infos error with expired initial agendas",
		agendasInitial: tAgendas,
		want:           tAgendas,
	}, {
		name:        "infos error with no initial agendas",
		deployments: tDeployments,
		want: &[]agenda{{
			Agenda: tDeployments[4][0],
		}, {
			Agenda: tDeployments[4][1],
		}},
	}, {
		name:           "within agendas life",
		agendasInitial: tAgendas,
		timerInitial:   time.Now().Add(agendasCacheLife),
		want:           tAgendas,
	}, {
		name: "no deployments",
		want: &[]agenda{},
	}}
	for _, test := range tests {
		agendasCache.Lock()
		agendasCache.agendas = test.agendasInitial
		agendasCache.timer = test.timerInitial
		agendasCache.Unlock()
		addr := "127.0.0.1:3000"
		done := func() {}
		if test.infos != nil {
			done = tServe(addr, test.infos)
		}
		params := &chaincfg.Params{Deployments: test.deployments}
		cfg := &Config{NetParams: params}
		mc := &MainController{Cfg: cfg, voteVersion: 4, DCRDataURL: "http://" + addr}
		agendas := mc.agendas()
		done()
		if !reflect.DeepEqual(agendas, test.want) {
			t.Fatalf("expected deployments %v but got %v for test %s", test.want, agendas, test.name)
		}
		agendasCache.Lock()
		// agendas always sets the agendasCache.
		if !reflect.DeepEqual(agendasCache.agendas, test.want) {
			t.Fatalf("expected deployments %v but got %v for agendasCache for test %s", test.want, agendasCache.agendas, test.name)
		}
		agendasCache.Unlock()
	}
}
