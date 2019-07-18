package email

import (
	"os"
	"testing"
)

type config struct {
	SMTPFrom          string `long:"smtpfrom" description:"From address to use on outbound mail"`
	SMTPHost          string `long:"smtphost" description:"SMTP hostname/ip and port, e.g. mail.example.com:25"`
	SMTPUsername      string `long:"smtpusername" description:"SMTP username for authentication if required"`
	SMTPPassword      string `long:"smtppassword" description:"SMTP password for authentication if required"`
	UseSMTPS          bool   `long:"usesmtps" description:"Connect to the SMTP server using smtps."`
	SMTPSkipTLSVerify bool   `long:"smtpskiptlsverify" description:"Skip TLS verification when it establish connection to the SMTP server."`
}

func TestSMTPSendMail(t *testing.T) {
	cfg := config{
		SMTPHost:     os.Getenv("SMTPHost"),
		SMTPUsername: os.Getenv("SMTPUsername"),
		SMTPPassword: os.Getenv("SMTPPassword"),
		SMTPFrom:     os.Getenv("SMTPFrom"),
		UseSMTPS:     false,
	}

	if cfg.SMTPHost == "" || cfg.SMTPUsername == "" || cfg.SMTPPassword == "" || cfg.SMTPFrom == "" {
		t.Skipped()
		return
	}

	sender, err := NewSender(cfg.SMTPHost, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom, cfg.UseSMTPS)
	if err != nil {
		t.Fatalf("Failed to initialize the smtp server: %v", err)
	}

	toEmail := "example@example.com"
	if err := sender.sendMail(toEmail, "testsubject", "testbody"); err != nil {
		t.Fatalf("Failed to initialize the smtp server: %v", sender)
	}
}
