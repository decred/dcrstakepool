package userdata

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

// DBConfig stores DB login information.
type DBConfig struct {
	DBHost     string
	DBName     string
	DBPassword string
	DBPort     string
	DBUser     string
}

// UserData stores the current snapshot of the user voting config.
type UserData struct {
	sync.RWMutex
	DBConfig         *DBConfig
	UserVotingConfig map[string]UserVotingConfig // [multisigaddr]
}

// UserVotingConfig contains per-user voting preferences.
type UserVotingConfig struct {
	Userid          int64
	MultiSigAddress string
	VoteBits        uint16
	VoteBitsVersion uint32
}

// MySQLFetchAddedLowFeeTickets fetches any low fee tickets that were
// manually added by the admin.
func (u *UserData) MySQLFetchAddedLowFeeTickets() (map[chainhash.Hash]string, error) {
	var (
		ticketHashString string
		ticketAddress    string
	)

	tickets := make(map[chainhash.Hash]string)

	db, err := sql.Open("mysql", fmt.Sprint(u.DBConfig.DBUser, ":", u.DBConfig.DBPassword, "@(", u.DBConfig.DBHost, ":", u.DBConfig.DBPort, ")/", u.DBConfig.DBName, "?charset=utf8mb4"))
	if err != nil {
		log.Errorf("Unable to open db: %v", err)
		return tickets, err
	}

	// sql.Open just validates its arguments without creating a connection
	// Verify that the data source name is valid with Ping:
	if err = db.Ping(); err != nil {
		log.Errorf("Unable to establish connection to db: %v", err)
		db.Close()
		return tickets, err
	}

	rows, err := db.Query("SELECT TicketHash, TicketAddress FROM LowFeeTicket")
	if err != nil {
		log.Errorf("Unable to query db: %v", err)
		db.Close()
		return tickets, err
	}

	for rows.Next() {
		err := rows.Scan(&ticketHashString, &ticketAddress)
		if err != nil {
			log.Errorf("Unable to scan row %v", err)
			continue
		}
		ticketHash, err := chainhash.NewHashFromStr(ticketHashString)
		if err != nil {
			log.Warnf("NewHashFromStr failed for %v: %v", err)
			continue
		}
		tickets[*ticketHash] = ticketAddress
	}
	if err = rows.Err(); err != nil {
		db.Close()
		return tickets, err
	}

	return tickets, db.Close()
}

// MySQLFetchUserVotingConfig fetches the voting preferences of all users
// who have completed registration of the pool by submitting an address
// and generating a multisig ticket address.
func (u *UserData) MySQLFetchUserVotingConfig() (map[string]UserVotingConfig, error) {
	var (
		userid          int64
		multiSigAddress string
		voteBits        int64
		voteBitsVersion int64
	)

	userInfo := map[string]UserVotingConfig{}

	db, err := sql.Open("mysql", fmt.Sprint(u.DBConfig.DBUser, ":", u.DBConfig.DBPassword, "@(", u.DBConfig.DBHost, ":", u.DBConfig.DBPort, ")/", u.DBConfig.DBName, "?charset=utf8mb4"))
	if err != nil {
		log.Errorf("Unable to open db: %v", err)
		return userInfo, err
	}

	// sql.Open just validates its arguments without creating a connection
	// Verify that the data source name is valid with Ping:
	if err = db.Ping(); err != nil {
		log.Errorf("Unable to establish connection to db: %v", err)
		db.Close()
		return userInfo, err
	}

	rows, err := db.Query("SELECT UserId, MultiSigAddress, VoteBits, VoteBitsVersion FROM Users WHERE MultiSigAddress <> ''")
	if err != nil {
		log.Errorf("Unable to query db: %v", err)
		db.Close()
		return userInfo, err
	}

	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&userid, &multiSigAddress, &voteBits, &voteBitsVersion)
		if err != nil {
			log.Errorf("Unable to scan row %v", err)
			continue
		}
		userInfo[multiSigAddress] = UserVotingConfig{
			Userid:          userid,
			MultiSigAddress: multiSigAddress,
			VoteBits:        uint16(voteBits),
			VoteBitsVersion: uint32(voteBitsVersion),
		}
	}
	if err = rows.Err(); err != nil {
		db.Close()
		return userInfo, err
	}

	return userInfo, db.Close()
}

// DBSetConfig sets the database configuration.
func (u *UserData) DBSetConfig(DBUser string, DBPassword string, DBHost string, DBPort string, DBName string) {
	dbconfig := &DBConfig{
		DBHost:     DBHost,
		DBName:     DBName,
		DBPassword: DBPassword,
		DBPort:     DBPort,
		DBUser:     DBUser,
	}
	u.Lock()
	u.DBConfig = dbconfig
	u.Unlock()
}
