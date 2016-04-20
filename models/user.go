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

type User struct {
	Id              int64 `db:"UserId"`
	Email           string
	Username        string
	Password        []byte
	MultiSigAddress string
	MultiSigScript  string
	PoolPubKeyAddr  string
	UserPubKeyAddr  string
	UserFeeAddr     string
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

func UpdateUserById(dbMap *gorp.DbMap, id int64, msa string, mss string, ppka string, upka string, ufa string) (user *User) {
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)

	if err != nil {
		glog.Warningf("Can't get user by id: %v", err)
	}

	user.MultiSigAddress = msa
	user.MultiSigScript = mss
	user.PoolPubKeyAddr = ppka
	user.UserPubKeyAddr = upka
	user.UserFeeAddr = ufa

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

	// add a table, setting the table name to 'posts' and
	// specifying that the Id property is an auto incrementing PK
	dbMap.AddTableWithName(User{}, "Users").SetKeys(true, "Id")

	// create the table. in a production system you'd generally
	// use a migration tool, or create the tables via scripts
	err = dbMap.CreateTablesIfNotExists()
	checkErr(err, "Create tables failed")

	return dbMap
}

func checkErr(err error, msg string) {
	if err != nil {
		log.Fatalln(msg, err)
	}
}
