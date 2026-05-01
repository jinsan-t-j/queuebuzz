package email

import (
	"fmt"
	"net/smtp"
)

// MailpitProvider implements Provider for Mailpit (local testing)
type MailpitProvider struct {
	Host      string
	Port      string
	FromEmail string
}

func (p *MailpitProvider) Send(to, subject, html string) error {
	message := []byte(fmt.Sprintf("Subject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nFrom: %s\r\nTo: %s\r\n\r\n%s", subject, p.FromEmail, to, html))

	addr := fmt.Sprintf("%s:%s", p.Host, p.Port)
	return smtp.SendMail(addr, nil, p.FromEmail, []string{to}, message)
}
