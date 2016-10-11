package helpers

import (
	"github.com/decred/dcrstakepool/models"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/gorp.v1"
)

func AddPasswordResetToken(dbMap *gorp.DbMap, email string) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE Email = ?", email)
	if err != nil {
		return nil, err
	}

	return &user, err
}

func EmailExists(dbMap *gorp.DbMap, email string) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE Email = ?", email)
	if err != nil {
		return nil, err
	}

	return &user, err
}

func TokenDelete(dbMap *gorp.DbMap, token string) error {
	_, err := dbMap.Exec("DELETE FROM PasswordReset WHERE Token = ?", token)
	return err
}

func TokenExists(dbMap *gorp.DbMap, token string) (*models.PasswordReset, error) {
	var passwordReset models.PasswordReset
	err := dbMap.SelectOne(&passwordReset, "SELECT * FROM PasswordReset WHERE Token = ?", token)
	if err != nil {
		return nil, err
	}

	return &passwordReset, err
}

func UpdateUserPasswordById(dbMap *gorp.DbMap, id int64, password []byte) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)
	if err != nil {
		return nil, err
	}

	user.Password = password

	_, err = dbMap.Update(&user)
	if err != nil {
		return nil, err
	}

	return &user, err
}

func UserIDExists(dbMap *gorp.DbMap, userid int64) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", userid)
	if err != nil {
		return nil, err
	}

	return &user, err
}

func Login(dbMap *gorp.DbMap, email string, password string) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE Email = ?", email)
	if err != nil {
		return nil, err
	}

	err = bcrypt.CompareHashAndPassword(user.Password, []byte(password))
	if err != nil {
		return nil, err
	}
	return &user, err
}
