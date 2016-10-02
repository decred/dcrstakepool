package models

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang/glog"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/gorp.v1"
)

type EmailChange struct {
	Id       int64 `db:"EmailChangeID"`
	UserId   int64
	NewEmail string
	Token    string
	Created  int64
	Expires  int64
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
}

func (user *User) HashPassword(password string) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		glog.Fatalf("Couldn't hash password: %v", err)
		panic(err)
	}
	user.Password = hash
}

func GetUserByEmail(dbMap *gorp.DbMap, email string) (user *User) {
	err := dbMap.SelectOne(&user, "SELECT * FROM Users where Email = ?", email)

	if err != nil {
		glog.Warningf("Can't get user by email: %v", err)
	}
	return
}

func GetUserById(dbMap *gorp.DbMap, id int64) (user *User) {
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)

	if err != nil {
		glog.Warningf("Can't get user by id: %v", err)
	}
	return
}

// GetUserCount gives a count of all users
func GetUserCount(dbMap *gorp.DbMap) int64 {
	userCount, err := dbMap.SelectInt("SELECT COUNT(*) FROM Users")
	if err != nil {
		return int64(0)
	}

	return userCount
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

// InsertUser inserts a user into the DB
func InsertUser(dbMap *gorp.DbMap, user *User) error {
	return dbMap.Insert(user)
}

func InsertPasswordReset(dbMap *gorp.DbMap, passwordReset *PasswordReset) error {
	return dbMap.Insert(passwordReset)
}

// UpdateUserByID updates a user, specified by id, in the DB with a new
// multiSigAddr, multiSigScript, multiSigScript, pool pubkey address,
// user pub key address, and fee address.  Unchanged are the user's ID, email,
// username and password.
func UpdateUserByID(dbMap *gorp.DbMap, id int64, multiSigAddr string,
	multiSigScript string, poolPubKeyAddr string, userPubKeyAddr string,
	userFeeAddr string) (user *User) {
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)

	if err != nil {
		glog.Warningf("Can't get user by id: %v", err)
	}

	user.MultiSigAddress = multiSigAddr
	user.MultiSigScript = multiSigScript
	user.PoolPubKeyAddr = poolPubKeyAddr
	user.UserPubKeyAddr = userPubKeyAddr
	user.UserFeeAddr = userFeeAddr
	user.HeightRegistered = height

	_, err = dbMap.Update(user)

	if err != nil {
		glog.Warningf("Couldn't update user: %v", err)
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

func GetDbMap(user, password, hostname, port, database string) *gorp.DbMap {
	// connect to db using standard Go database/sql API
	// use whatever database/sql driver you wish
	//TODO: Get user, password and database from config.
	db, err := sql.Open("mysql", fmt.Sprint(user, ":", password, "@(", hostname, ":", port, ")/", database, "?charset=utf8mb4"))
	checkErr(err, "sql.Open failed")

	// construct a gorp DbMap
	dbMap := &gorp.DbMap{Db: db, Dialect: gorp.MySQLDialect{Engine: "InnoDB", Encoding: "UTF8MB4"}}

	// add a table, setting the table name and specifying that
	// the Id property is an auto incrementing primary key
	dbMap.AddTableWithName(EmailChange{}, "EmailChange").SetKeys(true, "Id")
	dbMap.AddTableWithName(User{}, "Users").SetKeys(true, "Id")
	dbMap.AddTableWithName(PasswordReset{}, "PasswordReset").SetKeys(true, "Id")

	// create the table. in a production system you'd generally
	// use a migration tool, or create the tables via scripts
	err = dbMap.CreateTablesIfNotExists()
	checkErr(err, "Create tables failed")

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
	checkErr(err, "setting HeightRegistered to 0 failed")

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
		_, err = dbMap.Exec("ALTER TABLE Users ADD COLUMN `" +
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
		log.Fatalln(msg, err)
	}
}
