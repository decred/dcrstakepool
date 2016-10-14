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

func InsertUser(dbMap *gorp.DbMap, user *User) error {
	return dbMap.Insert(user)
}

func InsertPasswordReset(dbMap *gorp.DbMap, passwordReset *PasswordReset) error {
	return dbMap.Insert(passwordReset)
}

func UpdateUserById(dbMap *gorp.DbMap, id int64, msa string, mss string, ppka string, upka string, ufa string, height int64) (user *User) {
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)

	if err != nil {
		glog.Warningf("Can't get user by id: %v", err)
	}

	user.MultiSigAddress = msa
	user.MultiSigScript = mss
	user.PoolPubKeyAddr = ppka
	user.UserPubKeyAddr = upka
	user.UserFeeAddr = ufa
	user.HeightRegistered = height

	_, err = dbMap.Update(user)

	if err != nil {
		glog.Warningf("Couldn't update user: %v", err)
	}
	return
}

func GetDbMap(user, password, hostname, port, database string) *gorp.DbMap {
	// connect to db using standard Go database/sql API
	// use whatever database/sql driver you wish
	//TODO: Get user, password and database from config.
	db, err := sql.Open("mysql", fmt.Sprint(user, ":", password, "@(", hostname, ":", port, ")/", database, "?charset=utf8mb4"))
	checkErr(err, "sql.Open failed")

	// construct a gorp DbMap
	dbMap := &gorp.DbMap{Db: db, Dialect: gorp.MySQLDialect{Engine: "InnoDB", Encoding: "UTF8MB4"}}

	// add a table, setting the table name to Users and
	// specifying that the Id property is an auto incrementing PK
	dbMap.AddTableWithName(User{}, "Users").SetKeys(true, "Id")

	dbMap.AddTableWithName(PasswordReset{}, "PasswordReset").SetKeys(true, "Id")

	// create the table. in a production system you'd generally
	// use a migration tool, or create the tables via scripts
	err = dbMap.CreateTablesIfNotExists()
	checkErr(err, "Create tables failed")

	// The ORM, Gorp, doesn't support migrations so we just add new columns
	// that weren't present in the original schema so admins can upgrade
	// without manual intervention.

	// 1) stakepool v0.0.1 -> v0.0.2: add HeightRegistered so importscript
	//    doesn't take as long
	// The stake pool code was released to stake pool operators on Friday,
	// April 1st 2016.  The last mainnet block on Mar 31st of 15346 is used
	// as a safe default to ensure no tickets are missed. This could be
	// adjusted upwards since most pools were on testnet for a long time.

	// Determine if the HeightRegistered column exists
	s, err := dbMap.SelectStr("SELECT column_name FROM information_schema.columns " +
		"WHERE table_schema = '" + database +
		"' AND table_name = 'Users' AND column_name = 'HeightRegistered'")
	if s == "" {
		// HeightRegistered column doesn't exist so add it
		_, err = dbMap.Exec("ALTER TABLE Users ADD COLUMN `HeightRegistered` bigint(20) NULL AFTER `UserFeeAddr`")
		checkErr(err, "adding new column HeightRegistered failed")
		// set height to 15346 for all users who submitted a script
		_, err = dbMap.Exec("UPDATE Users SET HeightRegistered = 15346 WHERE MultiSigAddress <> ''")
		checkErr(err, "setting HeightRegistered default value failed")
	}
	return dbMap
}

func checkErr(err error, msg string) {
	if err != nil {
		log.Fatalln(msg, err)
	}
}
