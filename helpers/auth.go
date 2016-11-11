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

func EmailChangeComplete(dbMap *gorp.DbMap, token string) error {
	var emailChange models.EmailChange

	err := dbMap.SelectOne(&emailChange, "SELECT * FROM EmailChange WHERE Token = ?", token)
	if err != nil {
		return err
	}

	_, err = dbMap.Exec("UPDATE Users SET Email = ? WHERE UserId = ?",
		emailChange.NewEmail, emailChange.UserId)
	if err != nil {
		return err
	}

	_, err = dbMap.Exec("DELETE FROM EmailChange WHERE Token = ?", token)
	if err != nil {
		return err
	}

	return nil
}

func EmailChangeTokenExists(dbMap *gorp.DbMap, token string) (*models.EmailChange, error) {
	var emailChange models.EmailChange
	err := dbMap.SelectOne(&emailChange, "SELECT * FROM EmailChange WHERE Token = ?", token)
	if err != nil {
		return nil, err
	}

	return &emailChange, err
}

func EmailExists(dbMap *gorp.DbMap, email string) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE Email = ?", email)
	if err != nil {
		return nil, err
	}

	return &user, err
}

func EmailVerificationTokenExists(dbMap *gorp.DbMap, token string) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE EmailToken = ?", token)
	if err != nil {
		return nil, err
	}

	return &user, err
}

func EmailVerificationComplete(dbMap *gorp.DbMap, token string) error {
	_, err := dbMap.Exec("UPDATE Users SET EmailToken = '', "+
		"EmailVerified = 1 WHERE EmailToken = ?", token)
	return err
}

func PasswordResetTokenDelete(dbMap *gorp.DbMap, token string) error {
	_, err := dbMap.Exec("DELETE FROM PasswordReset WHERE Token = ?", token)
	return err
}

func PasswordResetTokenExists(dbMap *gorp.DbMap, token string) (*models.PasswordReset, error) {
	var passwordReset models.PasswordReset
	err := dbMap.SelectOne(&passwordReset, "SELECT * FROM PasswordReset WHERE Token = ?", token)
	if err != nil {
		return nil, err
	}

	return &passwordReset, err
}

func PasswordValidById(dbMap *gorp.DbMap, id int64, password string) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)
	if err != nil {
		return nil, err
	}

	err = bcrypt.CompareHashAndPassword(user.Password, []byte(password))
	if err != nil {
		return nil, err
	}
	return &user, err
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

// Login looks up a user by email and validates the provided clear text password
// against the bcrypt hashed password stored in the DB. Returns the *User and an
// error. On failure *User is nil and error is non-nil. On success, error is
// nil.
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
