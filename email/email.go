// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package email

import (
	"crypto/tls"
	"fmt"

	"github.com/dajohi/goemail"
)

type Sender struct {
	smtpFrom   string
	smtpServer *goemail.SMTP
}

func NewSender(smtpHost string, smtpUsername string, smtpPassword string,
	smtpFrom string, useSMTPS bool) (Sender, error) {
	// Format: smtp://[username[:password]@]host
	smtpURL := "smtp://"
	if useSMTPS {
		smtpURL = "smtps://"
	}

	if smtpUsername != "" {
		smtpURL += smtpUsername
		if smtpPassword != "" {
			smtpURL += ":" + smtpPassword
		}
		smtpURL += "@"
	}
	smtpURL += smtpHost

	tlsConfig := tls.Config{}
	smtpServer, err := goemail.NewSMTP(smtpURL, &tlsConfig)
	if err != nil {
		return Sender{}, err
	}

	// Validate smtpFrom address
	mailMsg := goemail.NewMessage(smtpFrom, "", "")
	if mailMsg == nil {
		return Sender{}, fmt.Errorf(`invalid smtpfrom address "%s"`, smtpFrom)
	}

	return Sender{
		smtpServer: smtpServer,
		smtpFrom:   smtpFrom,
	}, nil
}

// SendMail sends an email with the passed data using the system's SMTP
// configuration.
func (s *Sender) sendMail(emailaddress, subject, body string) error {
	// Connect to the server, authenticate, set the sender and recipient,
	// and send the email all in one step.
	mailMsg := goemail.NewMessage(s.smtpFrom, subject, body)
	if mailMsg == nil {
		return fmt.Errorf(`invalid smtpfrom address "%s"`, s.smtpFrom)
	}
	mailMsg.AddTo(emailaddress)

	return s.smtpServer.Send(mailMsg)
}

func (s *Sender) PasswordChangeRequest(email, clientIP, baseURL, token string) error {
	body := "A request to reset your password was made from IP address: " +
		clientIP + "\r\n\n" +
		"If you made this request, follow the link below:\r\n\n" +
		baseURL + "/passwordupdate?t=" + token + "\r\n\n" +
		"The above link expires an hour after this email was sent.\r\n\n" +
		"If you did not make this request, you may safely ignore this " +
		"email. However, you may want to look into how this " +
		"happened.\r\n"

	return s.sendMail(email, "Voting service password reset", body)
}

func (s *Sender) EmailChangeVerification(baseURL, currentEmail, newEmail, clientIP, token string) error {
	body := "A request was made to change the email address " +
		"for a voting service account at " + baseURL +
		" from " + currentEmail + " to " + newEmail + "\r\n\n" +
		"The request was made from IP address " + clientIP + "\r\n\n" +
		"If you made this request, follow the link below:\r\n\n" +
		baseURL + "/emailupdate?t=" + token + "\r\n\n" +
		"The above link expires an hour after this email was sent.\r\n\n" +
		"If you did not make this request, you may safely ignore this " +
		"email. However, you may want to look into how this happened.\r\n"

	return s.sendMail(newEmail, "Voting service email change", body)
}

func (s *Sender) EmailChangeNotification(baseURL, currentEmail, newEmail, clientIP string) error {
	body := "A request was made to change the email address " +
		"for your voting service account at " + baseURL +
		" from " + currentEmail + " to " + newEmail + "\r\n\n" +
		"The request was made from IP address " + clientIP + "\r\n\n" +
		"If you did not make this request, please contact the" +
		"Voting service administrator immediately.\r\n"

	return s.sendMail(currentEmail, "Voting service email change", body)
}

func (s *Sender) PasswordChangeConfirm(email, baseURL, clientIP string) error {
	body := "Your voting service password for " + baseURL +
		" was just changed by IP Address " + clientIP + "\r\n\n" +
		"If you did not make this request, please contact the " +
		"Voting service administrator immediately.\r\n"

	return s.sendMail(email, "Voting service password change", body)
}

func (s *Sender) Registration(email, baseURL, clientIP, token string) error {
	body := "A request for an account for " + baseURL + " was made from " +
		clientIP + " for this email address.\r\n\n" +
		"If you made this request, follow the link below " +
		"to verify your email address and finalize registration:\r\n\n" +
		baseURL + "/emailverify?t=" + token + "\r\n"

	return s.sendMail(email, "Voting service provider email verification", body)
}
