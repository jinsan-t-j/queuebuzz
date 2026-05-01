package services

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/log"
	systemservice "queuebuzz/internal/modules/system/service"
	"queuebuzz/internal/services/email"
)

type EmailService struct {
	cfg           *config.Config
	provider      email.Provider
	systemService *systemservice.SystemService
	templates     map[string]*template.Template
	disposableMap map[string]bool
	allowedMap    map[string]bool
	supportEmail  string
	job           *email.Job
	mu            sync.RWMutex
}

const blocklistURL = "https://raw.githubusercontent.com/disposable-email-domains/disposable-email-domains/refs/heads/main/disposable_email_blocklist.conf"

//go:embed email/disposable_blocklist.conf
var disposableBlocklistRaw string

func NewEmailService(cfg *config.Config, provider email.Provider, sys *systemservice.SystemService) *EmailService {
	s := &EmailService{
		cfg:           cfg,
		provider:      provider,
		systemService: sys,
		templates:     make(map[string]*template.Template),
		disposableMap: make(map[string]bool),
		allowedMap:    make(map[string]bool),
		supportEmail:  cfg.SupportEmail,
	}

	// Initial load from embedded file (fast & reliable)
	scanner := bufio.NewScanner(strings.NewReader(disposableBlocklistRaw))
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain != "" && !strings.HasPrefix(domain, "#") {
			s.disposableMap[strings.ToLower(domain)] = true
		}
	}

	// Load templates
	_, b, _, _ := runtime.Caller(0)
	basepath := filepath.Dir(b)
	templatesDir := filepath.Join(basepath, "email/templates")
	layoutPath := filepath.Join(templatesDir, "layout.gohtml")

	files, err := os.ReadDir(templatesDir)
	if err != nil {
		log.Error().Err(err).Str("path", templatesDir).Msg("Failed to read templates directory")
		return s
	}

	for _, f := range files {
		if f.IsDir() || f.Name() == "layout.gohtml" || filepath.Ext(f.Name()) != ".gohtml" {
			continue
		}

		name := f.Name()
		fullPath := filepath.Join(templatesDir, name)

		tmpl := template.New(name)
		tmpl, err = tmpl.ParseFiles(layoutPath, fullPath)
		if err != nil {
			log.Error().Err(err).Str("file", name).Msg("Failed to parse email template")
			continue
		}
		s.templates[name] = tmpl
	}

	// Initialize job
	s.job = email.NewJob(s)

	return s
}

func (s *EmailService) Job() *email.Job {
	return s.job
}

// SendMagicLink sends a magic link email for host registration.
func (s *EmailService) SendMagicLink(toEmail, token string) error {
	data := map[string]interface{}{
		"Link": fmt.Sprintf("%s/auth/verify?token=%s", s.cfg.AppURL, token),
		"Year": time.Now().Year(),
	}

	html, err := s.renderTemplate("magic_link.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "Sign in to QueueBuzz", html)
	return nil
}

// SendBuzzFallback sends an email notification when FCM delivery fails.
func (s *EmailService) SendBuzzFallback(toEmail, ticketNo, queueName, queueID string) error {
	data := map[string]interface{}{
		"TicketNo":  ticketNo,
		"QueueName": queueName,
		"Link":      fmt.Sprintf("%s/q/%s/waiting", s.cfg.AppURL, queueID),
		"Year":      time.Now().Year(),
	}

	html, err := s.renderTemplate("buzz_fallback.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "It's your turn! 🎉", html)
	return nil
}

// SendQueueEnded sends an email notification when a queue session is closed by the host.
func (s *EmailService) SendQueueEnded(toEmail, queueName string) error {
	data := map[string]interface{}{
		"QueueName": queueName,
		"Year":      time.Now().Year(),
	}

	html, err := s.renderTemplate("queue_ended.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, fmt.Sprintf("Queue Ended: %s", queueName), html)
	return nil
}

func (s *EmailService) renderTemplate(name string, data interface{}) (string, error) {
	tmpl, ok := s.templates[name]
	if !ok {
		return "", fmt.Errorf("template %s not found", name)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("failed to render template %s: %w", name, err)
	}

	return buf.String(), nil
}

// SendImmediate implements email.Sender
func (s *EmailService) SendImmediate(to, subject, html string) error {

	if s.provider == nil {
		return fmt.Errorf("email provider not configured")
	}

	return s.provider.Send(to, subject, html)
}

// ValidateEmail checks if an email is valid, has a working DNS MX record,
// and is not from a known disposable email provider (unless whitelisted).
func (s *EmailService) ValidateEmail(emailAddr string) error {
	// 1. Basic format check
	parts := strings.Split(emailAddr, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid email format")
	}
	domain := strings.ToLower(parts[1])

	// 2. Check for allowed domains (Whitelist bypass)
	s.mu.RLock()
	isAllowed := s.allowedMap[domain]
	s.mu.RUnlock()

	if isAllowed {
		return nil
	}

	// 3. Check for disposable email domains
	if s.isDisposable(domain) {
		s.mu.RLock()
		email := s.supportEmail
		s.mu.RUnlock()

		if email == "" {
			email = s.cfg.SupportEmail
		}
		return fmt.Errorf("this email provider is not allowed. if you believe this is a mistake, please contact us at %s", email)
	}

	// 4. DNS check (Only in production)
	if s.cfg.IsProduction() {
		// Try MX records first
		mx, err := net.LookupMX(domain)
		if err == nil && len(mx) > 0 {
			return nil
		}

		// Fallback to A record check (RFC 5321 compliance)
		ips, err := net.LookupIP(domain)
		if err != nil || len(ips) == 0 {
			return fmt.Errorf("the domain %s is unreachable or has no valid email records", domain)
		}
	}

	return nil
}

func (s *EmailService) isDisposable(domain string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.disposableMap[strings.ToLower(domain)]
}

// RefreshBlocklist fetches the latest disposable email domains from GitHub
func (s *EmailService) RefreshBlocklist() error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(blocklistURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch blocklist: status %d", resp.StatusCode)
	}

	newMap := make(map[string]bool)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain != "" && !strings.HasPrefix(domain, "#") {
			newMap[strings.ToLower(domain)] = true
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Fetch Allowed domains and Support Email from DB
	newAllowedMap := make(map[string]bool)
	supportEmail := s.cfg.SupportEmail // Fallback from config
	if s.systemService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		settings, err := s.systemService.GetSettings(ctx)
		if err == nil {
			for _, d := range settings.AllowedEmailDomains {
				newAllowedMap[strings.ToLower(strings.TrimSpace(d))] = true
			}
			if settings.SupportEmail != "" {
				supportEmail = settings.SupportEmail
			}
		}
	}

	s.mu.Lock()
	s.disposableMap = newMap
	s.allowedMap = newAllowedMap
	s.supportEmail = supportEmail
	s.mu.Unlock()

	log.Info().Msgf("email blocklist refreshed: %d blocked, %d allowed domains", len(newMap), len(newAllowedMap))
	return nil
}
