package models

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-gorp/gorp"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

type EmailChange struct {
	Id       int64 `db:"EmailChangeID"`
	UserId   int64
	NewEmail string
	Token    string
	Created  int64
	Expires  int64
}

type LowFeeTicket struct {
	Id            int64 `db:"LowFeeTicketID"`
	AddedByUid    int64
	TicketAddress string
	TicketHash    string
	TicketExpiry  int64
	Voted         int64
	Created       int64
	Expires       int64
}

type PasswordReset struct {
	Id      int64 `db:"PasswordResetID"`
	UserId  int64
	Token   string
	Created int64
	Expires int64
}

type User struct {
	Id               int64 `db:"UserId"`
	Email            string
	Username         string
	Password         []byte
	MultiSigAddress  string
	MultiSigScript   string
	PoolPubKeyAddr   string
	UserPubKeyAddr   string
	UserFeeAddr      string
	HeightRegistered int64
	EmailVerified    int64
	EmailToken       string
	APIToken         string
	VoteBits         int64
	VoteBitsVersion  int64
}

func (user *User) HashPassword(password string) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Criticalf("Couldn't hash password: %v", err)
		panic(err)
	}
	user.Password = hash
}

func GetUserByEmail(dbMap *gorp.DbMap, email string) (user *User) {
	err := dbMap.SelectOne(&user, "SELECT * FROM Users where Email = ?", email)

	if err != nil {
		log.Warnf("Can't get user by email: %v", err)
	}
	return
}

func GetUserById(dbMap *gorp.DbMap, id int64) (user *User, err error) {
	err = dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)

	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserCount gives a count of all users
func GetUserCount(dbMap *gorp.DbMap) int64 {
	userCount, err := dbMap.SelectInt("SELECT COUNT(*) FROM Users")
	if err != nil {
		return int64(0)
	}

	return userCount
}

// GetUserMax gives the last userid
func GetUserMax(dbMap *gorp.DbMap) int64 {
	maxUserID, err := dbMap.SelectInt("SELECT MAX(Userid) FROM Users")
	if err != nil {
		return int64(0)
	}

	return maxUserID
}

// GetUserCountActive gives a count of all users who have submitted an address
func GetUserCountActive(dbMap *gorp.DbMap) int64 {
	userCountActive, err := dbMap.SelectInt("SELECT COUNT(*) FROM Users " +
		"WHERE MultiSigAddress <> ''")
	if err != nil {
		return int64(0)
	}

	return userCountActive
}

func InsertEmailChange(dbMap *gorp.DbMap, emailChange *EmailChange) error {
	return dbMap.Insert(emailChange)
}

// InsertLowFeeTicket inserts a user into the DB
func InsertLowFeeTicket(dbMap *gorp.DbMap, lowFeeTicket *LowFeeTicket) error {
	return dbMap.Insert(lowFeeTicket)
}

// InsertUser inserts a user into the DB
func InsertUser(dbMap *gorp.DbMap, user *User) error {
	return dbMap.Insert(user)
}

func InsertPasswordReset(dbMap *gorp.DbMap, passwordReset *PasswordReset) error {
	return dbMap.Insert(passwordReset)
}

// SetUserAPIToken generates and saves a unique API Token for a user.
func SetUserAPIToken(dbMap *gorp.DbMap, APISecret string, baseURL string,
	id int64) error {
	var user *User
	token := jwt.New(jwt.SigningMethodHS256)

	claims := make(jwt.MapClaims)
	claims["iat"] = time.Now().Unix()
	claims["iss"] = baseURL
	claims["loggedInAs"] = id

	token.Claims = claims

	tokenString, err := token.SignedString([]byte(APISecret))
	if err != nil {
		return err
	}

	err = dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)
	user.APIToken = tokenString
	if err != nil {
		return err
	}

	_, err = dbMap.Update(user)
	return err
}

// UpdateUserByID updates a user, specified by id, in the DB with a new
// multiSigAddr, multiSigScript, multiSigScript, pool pubkey address,
// user pub key address, and fee address.  Unchanged are the user's ID, email,
// username and password.
func UpdateUserByID(dbMap *gorp.DbMap, id int64, multiSigAddr string,
	multiSigScript string, poolPubKeyAddr string, userPubKeyAddr string,
	userFeeAddr string, height int64) (user *User) {
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)

	if err != nil {
		log.Warnf("Can't get user by id: %v", err)
	}

	user.MultiSigAddress = multiSigAddr
	user.MultiSigScript = multiSigScript
	user.PoolPubKeyAddr = poolPubKeyAddr
	user.UserPubKeyAddr = userPubKeyAddr
	user.UserFeeAddr = userFeeAddr
	user.HeightRegistered = height

	_, err = dbMap.Update(user)

	if err != nil {
		log.Warnf("Couldn't update user: %v", err)
	}

	// return updated User
	return
}

func GetAllCurrentMultiSigScripts(dbMap *gorp.DbMap) ([]User, error) {
	var multiSigs []User
	_, err := dbMap.Select(&multiSigs, "SELECT MultiSigScript, HeightRegistered FROM Users WHERE MultiSigAddress <> ''")
	if err != nil {
		return nil, err
	}
	return multiSigs, nil
}

func GetAllLowFeeTickets(dbMap *gorp.DbMap) ([]LowFeeTicket, error) {
	var lowFeeTickets []LowFeeTicket
	_, err := dbMap.Select(&lowFeeTickets, "SELECT * FROM LowFeeTicket")
	if err != nil {
		return nil, err
	}
	return lowFeeTickets, nil
}

func GetVotableLowFeeTickets(dbMap *gorp.DbMap) ([]LowFeeTicket, error) {
	var votableLowFeeTickets []LowFeeTicket
	_, err := dbMap.Select(&votableLowFeeTickets, "SELECT * FROM LowFeeTicket WHERE Voted = 0 AND Expires > UNIX_TIMESTAMP()")
	if err != nil {
		return nil, err
	}
	return votableLowFeeTickets, nil
}

func GetDbMap(APISecret, baseURL, user, password, hostname, port, database string) *gorp.DbMap {
	// connect to db using standard Go database/sql API
	// use whatever database/sql driver you wish
	db, err := sql.Open("mysql", fmt.Sprint(user, ":", password, "@(", hostname, ":", port, ")/", database, "?charset=utf8mb4"))
	if err != nil {
		log.Critical("sql.Open failed: ", err)
		return nil
	}

	// sql.Open just validates its arguments without creating a connection
	// Verify that the data source name is valid with Ping:
	if err = db.Ping(); err != nil {
		log.Critical("Unable to establish connection to database: ", err)
		return nil
	}

	// construct a gorp DbMap
	dbMap := &gorp.DbMap{Db: db, Dialect: gorp.MySQLDialect{Engine: "InnoDB", Encoding: "UTF8MB4"}}

	// add a table, setting the table name and specifying that
	// the Id property is an auto incrementing primary key
	dbMap.AddTableWithName(EmailChange{}, "EmailChange").SetKeys(true, "Id")
	dbMap.AddTableWithName(LowFeeTicket{}, "LowFeeTicket").SetKeys(true, "Id")
	dbMap.AddTableWithName(PasswordReset{}, "PasswordReset").SetKeys(true, "Id")
	dbMap.AddTableWithName(User{}, "Users").SetKeys(true, "Id")

	// create the table. in a production system you'd generally
	// use a migration tool, or create the tables via scripts
	err = dbMap.CreateTablesIfNotExists()
	if err != nil {
		log.Critical("Create tables failed: ", err)
		// There is no point proceeding, so return. TODO: signal to caller the
		// error, or possibly close the db, or panic.
		return dbMap
	}

	// The ORM, Gorp, doesn't support migrations so we just add new columns
	// that weren't present in the original schema so admins can upgrade
	// without manual intervention.

	// stakepool v0.0.1 -> v0.0.2
	// add HeightRegistered so dcrwallet doesn't scan from the genesis block
	// for transactions that won't exist.
	// The stake pool code was released to stake pool operators on Friday,
	// April 1st 2016.  The last mainnet block on Mar 31st of 15346 is used
	// as a safe default to ensure no tickets are missed. This could be
	// adjusted upwards since most pools were on testnet for a long time.
	addColumn(dbMap, database, "Users", "HeightRegistered", "bigint(20) NULL",
		"UserFeeAddr",
		"UPDATE Users SET HeightRegistered = 15346 WHERE MultiSigAddress <> ''")

	// stakepool v0.0.2 -> v0.0.3

	// bug fix for previous -- users who hadn't submitted a script won't be
	// able to login because Gorp can't handle NULL values
	_, err = dbMap.Exec("UPDATE Users SET HeightRegistered = 0 WHERE HeightRegistered IS NULL")
	if err != nil {
		log.Error("Setting HeightRegistered to 0 failed ", err)
		// Do not return since db is opened and other statements may work
	}

	// add EmailVerified, EmailToken so new users' email addresses can be
	// verified.  We consider users who already registered to be grandfathered
	// in and use 2 to reflect that.  1 is verified, 0 is unverified.
	addColumn(dbMap, database, "Users", "EmailVerified", "bigint(20) NULL",
		"HeightRegistered",
		"UPDATE Users SET EmailVerified = 2")
	// Set an empty token for grandfathered accounts
	addColumn(dbMap, database, "Users", "EmailToken", "varchar(255) NULL",
		"EmailVerified",
		"UPDATE Users SET EmailToken = ''")

	// stakepool v0.0.4 -> v1.0.0

	// add APIToken column for storing a token that users may use to submit a
	// public key address and retrieve ticket purchasing information via the API
	addColumn(dbMap, database, "Users", "APIToken", "varchar(255) NULL",
		"EmailToken", "UPDATE Users SET APIToken = ''")

	// Set an API token for all users who have verified their email address
	// and do not have an API Token already set.
	var users []User
	_, err = dbMap.Select(&users, "SELECT * FROM Users WHERE APIToken = '' AND EmailVerified > 0")
	if err != nil {
		log.Critical("Select verified users failed: ", err)
		// With out a Valid []Users, we cannot proceed
		return dbMap
	}

	for _, u := range users {
		err := SetUserAPIToken(dbMap, APISecret, baseURL, u.Id)
		if err != nil {
			log.Errorf("Unable to set API Token for UserId %v: %v", u.Id, err)
		}
	}

	// stakepool v1.0.0 -> v1.1.0

	// add VoteBits column for storing the user's voting preferences.  Set the
	// default to 1 which means the previous block was valid
	addColumn(dbMap, database, "Users", "VoteBits", "bigint(20) NULL", "APIToken", "UPDATE Users SET VoteBits = 1")

	// add VoteBitsVersion column for storing the vote version that the VoteBits
	// are set for.  The default is 3 since that is the current version on mainnet
	// and it will be upgraded when talking to stakepoold
	addColumn(dbMap, database, "Users", "VoteBitsVersion", "bigint(20) NULL", "VoteBits", "UPDATE Users SET VoteBitsVersion = 3")

	return dbMap
}

// addColumn checks if a column exists and adds it if it doesn't
func addColumn(dbMap *gorp.DbMap, db string, table string, columnToAdd string,
	dataSpec string, colAfter string, defaultQry string) {
	s, err := dbMap.SelectStr("SELECT column_name FROM " +
		"information_schema.columns WHERE table_schema = '" + db +
		"' AND table_name = '" + table + "' AND column_name = '" +
		columnToAdd + "'")
	checkErr(err, "checking whether column"+columnToAdd+" exists failed")
	if s == "" {
		// TODO would be nice to use parameter binding here but gorp seems to
		// only provide that for select queries
		_, err = dbMap.Exec("ALTER TABLE `" + table + "` ADD COLUMN `" +
			columnToAdd + "` " + dataSpec + " AFTER `" + colAfter + "`")
		checkErr(err, "adding new column "+columnToAdd+" failed")
		if defaultQry != "" {
			_, err = dbMap.Exec(defaultQry)
			checkErr(err, defaultQry+" failed")
		}
	}
}

func checkErr(err error, msg string) {
	if err != nil {
		log.Critical(msg, err)
	}
}
