package userdata

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
)

type DBConfig struct {
	DBHost     string
	DBName     string
	DBPassword string
	DBPort     string
	DBUser     string
}

// UserData stores the current snapshot of the user voting config.
type UserData struct {
	sync.Mutex
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

func (u *UserData) FetchUserVotingConfig() map[string]UserVotingConfig {
	var (
		Userid          int64
		MultiSigAddress string
		VoteBits        int64
		VoteBitsVersion int64
	)

	userInfo := map[string]UserVotingConfig{}

	db, err := sql.Open("mysql", fmt.Sprint(u.DBConfig.DBUser, ":", u.DBConfig.DBPassword, "@(", u.DBConfig.DBHost, ":", u.DBConfig.DBPort, ")/", u.DBConfig.DBName, "?charset=utf8mb4"))
	if err != nil {
		log.Errorf("Unable to open db: %v", err)
		return userInfo
	}

	// sql.Open just validates its arguments without creating a connection
	// Verify that the data source name is valid with Ping:
	if err = db.Ping(); err != nil {
		log.Errorf("Unable to establish connection to db: %v", err)
		return userInfo
	}

	rows, err := db.Query("SELECT UserId, MultiSigAddress, VoteBits, VoteBitsVersion FROM Users WHERE MultiSigAddress <> ''")
	if err != nil {
		log.Errorf("Unable to query db: %v", err)
		return userInfo
	}

	count := 0
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&Userid, &MultiSigAddress, &VoteBits, &VoteBitsVersion)
		if err != nil {
			log.Errorf("Unable to scan row %v", err)
			continue
		}
		userInfo[MultiSigAddress] = UserVotingConfig{
			Userid:          Userid,
			MultiSigAddress: MultiSigAddress,
			VoteBits:        uint16(VoteBits),
			VoteBitsVersion: uint32(VoteBitsVersion),
		}
		count++
	}

	err = db.Close()
	if err != nil {
		log.Errorf("Unable to close database: %v", err)
	}

	userNoun := pickNoun(count, "user", "users")
	log.Infof("fetch voting config for %d %s", count, userNoun)

	return userInfo
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

// Get returns the current user data.
func (u *UserData) Get() map[string]UserVotingConfig {
	u.Lock()
	data := u.UserVotingConfig
	u.Unlock()
	return data
}

// Update updates the current user data.
func (u *UserData) Update() error {
	newData := u.FetchUserVotingConfig()
	if newData == nil {
		return errors.New("unable to fetch user data")
	}

	u.Lock()
	u.UserVotingConfig = newData
	u.Unlock()
	return nil
}
