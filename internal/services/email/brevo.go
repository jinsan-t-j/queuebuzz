package email

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type BrevoProvider struct {
	APIKey    string
	FromEmail string
	FromName  string
	ReplyTo   string
}

type brevoPayload struct {
	Sender      brevoContact   `json:"sender"`
	To          []brevoContact `json:"to"`
	Subject     string         `json:"subject"`
	HTMLContent string         `json:"htmlContent"`
	ReplyTo     *brevoContact  `json:"replyTo,omitempty"`
}

type brevoContact struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

func (p *BrevoProvider) Send(to, subject, html string) error {
	payload := brevoPayload{
		Sender:      brevoContact{Email: p.FromEmail, Name: p.FromName},
		To:          []brevoContact{{Email: to}},
		Subject:     subject,
		HTMLContent: html,
	}

	if p.ReplyTo != "" {
		payload.ReplyTo = &brevoContact{Email: p.ReplyTo}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal brevo payload: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.brevo.com/v3/smtp/email", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", p.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call brevo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("brevo returned status %d", resp.StatusCode)
	}

	return nil
}
