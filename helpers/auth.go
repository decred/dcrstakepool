package helpers

import (
	"github.com/decred/dcrstakepool/models"
	"github.com/go-gorp/gorp"
	"golang.org/x/crypto/bcrypt"
)

func AddPasswordResetToken(dbMap *gorp.DbMap, email string) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM users WHERE email = ?;", email)
	if err != nil {
		return nil, err
	}

	return &user, err
}

func EmailChangeComplete(dbMap *gorp.DbMap, token string) error {
	var emailChange models.EmailChange

	err := dbMap.SelectOne(&emailChange, "SELECT * FROM email_change WHERE token = ?;", token)
	if err != nil {
		return err
	}

	_, err = dbMap.Exec("UPDATE users SET email = ? WHERE user_id = ?;",
		emailChange.NewEmail, emailChange.UserId)
	if err != nil {
		return err
	}

	_, err = dbMap.Exec("DELETE FROM email_change WHERE token = ?;", token)
	return err
}

func EmailChangeTokenExists(dbMap *gorp.DbMap, token string) (*models.EmailChange, error) {
	var emailChange models.EmailChange
	err := dbMap.SelectOne(&emailChange, "SELECT * FROM email_change WHERE token = ?;", token)
	if err != nil {
		return nil, err
	}

	return &emailChange, err
}

func EmailExists(dbMap *gorp.DbMap, email string) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM users WHERE email = ?;", email)
	if err != nil {
		return nil, err
	}

	return &user, err
}

func EmailVerificationTokenExists(dbMap *gorp.DbMap, token string) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM users WHERE email_token = ?;", token)
	if err != nil {
		return nil, err
	}

	return &user, err
}

func EmailVerificationComplete(dbMap *gorp.DbMap, token string) error {
	_, err := dbMap.Exec("UPDATE users SET email_token = '', "+
		"email_verified = 1 WHERE email_token = ?;", token)
	return err
}

func PasswordResetTokenDelete(dbMap *gorp.DbMap, token string) error {
	_, err := dbMap.Exec("DELETE FROM password_reset WHERE token = ?;", token)
	return err
}

func PasswordResetTokenExists(dbMap *gorp.DbMap, token string) (*models.PasswordReset, error) {
	var passwordReset models.PasswordReset
	err := dbMap.SelectOne(&passwordReset, "SELECT * FROM password_reset WHERE token = ?;", token)
	if err != nil {
		return nil, err
	}

	return &passwordReset, err
}

func PasswordValidById(dbMap *gorp.DbMap, id int64, password string) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM users WHERE user_id = ?;", id)
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
	err := dbMap.SelectOne(&user, "SELECT * FROM users WHERE user_id = ?;", id)
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
	err := dbMap.SelectOne(&user, "SELECT * FROM users WHERE user_id = ?;", userid)
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
	err := dbMap.SelectOne(&user, "SELECT * FROM users WHERE email = ?;", email)
	if err != nil {
		return nil, err
	}

	err = bcrypt.CompareHashAndPassword(user.Password, []byte(password))
	if err != nil {
		return nil, err
	}
	return &user, err
}
