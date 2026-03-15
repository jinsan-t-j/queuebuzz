package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/log"
)

type EmailService struct {
	cfg *config.Config
}

func NewEmailService(cfg *config.Config) *EmailService {
	return &EmailService{
		cfg: cfg,
	}
}

type resendPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	ReplyTo string   `json:"reply_to,omitempty"`
}

// SendMagicLink sends a magic link email for host registration.
func (s *EmailService) SendMagicLink(email, token string) error {
	link := fmt.Sprintf("%s/auth/verify?token=%s", s.cfg.AppURL, token)

	html := fmt.Sprintf(`
		<h2>Verify your QueueBuzz account</h2>
		<p>Click the link below to sign in to your QueueBuzz account:</p>
		<p><a href="%s" style="padding: 12px 24px; background-color: #4F46E5; color: white; text-decoration: none; border-radius: 6px; display: inline-block;">Sign In</a></p>
		<p>Or copy this link: <code>%s</code></p>
		<p>This link expires in 15 minutes and can only be used once.</p>
		<p style="color: #6b7280; font-size: 12px;">If you didn't request this, ignore this email.</p>
	`, link, link)

	return s.send(email, "Sign in to QueueBuzz", html)
}

// SendBuzzFallback sends an email notification when FCM delivery fails.
func (s *EmailService) SendBuzzFallback(email, ticketNo, queueInfo string) error {
	html := fmt.Sprintf(`
		<h2>You're up! 🎉</h2>
		<p>Your ticket <strong>%s</strong> has been called.</p>
		<p>%s</p>
		<p>Please head to the counter now.</p>
	`, ticketNo, queueInfo)

	return s.send(email, "QueueBuzz: You're being called!", html)
}

// SendQueueClosingWarning sends an email warning that a queue is about to close.
func (s *EmailService) SendQueueClosingWarning(email, ticketNo string) error {
	html := fmt.Sprintf(`
		<h2>Queue closing soon</h2>
		<p>The queue you're in (ticket %s) will be closing in 30 minutes.</p>
		<p>Please make sure you're available when your number is called.</p>
	`, ticketNo)

	return s.send(email, "QueueBuzz: Queue closing soon", html)
}

func (s *EmailService) send(to, subject, html string) error {
	if !s.cfg.IsProduction() {
		return s.sendViaMailpit(to, subject, html)
	}

	if s.cfg.ResendAPIKey == "" {
		return fmt.Errorf("resend api key is required outside development")
	}

	payload := resendPayload{
		From:    s.cfg.EmailFrom,
		To:      []string{to},
		Subject: subject,
		HTML:    html,
	}

	if s.cfg.EmailReplyTo != "" {
		payload.ReplyTo = s.cfg.EmailReplyTo
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal email payload: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create email request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.ResendAPIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// Log without email address per privacy rules
		log.Error().
			Int("status", resp.StatusCode).
			Str("subject", subject).
			Msg("Email delivery failed")
		return fmt.Errorf("email delivery failed with status %d", resp.StatusCode)
	}

	return nil
}

func (s *EmailService) sendViaMailpit(to, subject, html string) error {
	message := []byte(fmt.Sprintf("Subject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\nFrom: %s\r\nTo: %s\r\n\r\n%s", subject, s.cfg.EmailFrom, to, html))

	if err := smtp.SendMail(s.cfg.MailpitSMTPHost+":"+s.cfg.MailpitSMTPPort, nil, s.cfg.EmailFrom, []string{to}, message); err != nil {
		return fmt.Errorf("failed to send email via mailpit: %w", err)
	}

	return nil
}
