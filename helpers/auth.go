package helpers

import (
	"github.com/decred/dcrstakepool/models"
	"github.com/go-gorp/gorp"
	"golang.org/x/crypto/bcrypt"
)

func EmailChangeComplete(dbMap *gorp.DbMap, token models.UserToken) error {
	var emailChange models.EmailChange

	err := dbMap.SelectOne(&emailChange,
		"SELECT * FROM EmailChange WHERE Token = ?", token.String())
	if err != nil {
		return err
	}

	_, err = dbMap.Exec("UPDATE Users SET Email = ? WHERE UserId = ?",
		emailChange.NewEmail, emailChange.UserId)
	if err != nil {
		return err
	}

	_, err = dbMap.Exec("DELETE FROM PasswordReset WHERE UserId = ?", emailChange.UserId)
	if err != nil {
		return err
	}

	_, err = dbMap.Exec("DELETE FROM EmailChange WHERE Token = ?", token.String())
	return err
}

func EmailChangeTokenExists(dbMap *gorp.DbMap, token models.UserToken) (*models.EmailChange, error) {
	var emailChange models.EmailChange
	err := dbMap.SelectOne(&emailChange,
		"SELECT * FROM EmailChange WHERE Token = ?", token.String())
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

func EmailVerificationTokenExists(dbMap *gorp.DbMap, token models.UserToken) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user,
		"SELECT * FROM Users WHERE EmailToken = ?", token.String())
	if err != nil {
		return nil, err
	}

	return &user, err
}

func EmailVerificationComplete(dbMap *gorp.DbMap, token models.UserToken) error {
	_, err := dbMap.Exec(`UPDATE Users
		SET EmailToken = '', EmailVerified = 1
		WHERE EmailToken = ?`, token.String())
	return err
}

func PasswordResetTokenDelete(dbMap *gorp.DbMap, token models.UserToken) error {
	_, err := dbMap.Exec("DELETE FROM PasswordReset WHERE Token = ?", token.String())
	return err
}

func PasswordResetTokenExists(dbMap *gorp.DbMap, token models.UserToken) (*models.PasswordReset, error) {
	var passwordReset models.PasswordReset
	err := dbMap.SelectOne(&passwordReset,
		"SELECT * FROM PasswordReset WHERE Token = ?", token.String())
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

func UpdateVoteBitsByID(dbMap *gorp.DbMap, id int64, voteBits uint16) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)
	if err != nil {
		return nil, err
	}

	user.VoteBits = int64(voteBits)

	_, err = dbMap.Update(&user)
	if err != nil {
		return nil, err
	}

	return &user, err
}

func UpdateVoteBitsVersionByID(dbMap *gorp.DbMap, id int64, voteVersion uint32) (*models.User, error) {
	var user models.User
	err := dbMap.SelectOne(&user, "SELECT * FROM Users WHERE UserId = ?", id)
	if err != nil {
		return nil, err
	}

	user.VoteBitsVersion = int64(voteVersion)

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
