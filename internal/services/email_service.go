package services

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"math"
	"net"
	"net/http"
	"path/filepath"
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

//go:embed email/templates/*.gohtml
var templateFS embed.FS

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

	s.job = email.NewJob(s)

	// Initial load from embedded file (fast & reliable)
	scanner := bufio.NewScanner(strings.NewReader(disposableBlocklistRaw))
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain != "" && !strings.HasPrefix(domain, "#") {
			s.disposableMap[strings.ToLower(domain)] = true
		}
	}

	// Load templates from embedded filesystem
	files, err := templateFS.ReadDir("email/templates")
	if err != nil {
		log.Error().Err(err).Msg("Failed to read embedded templates directory")
		return s
	}

	for _, f := range files {
		if f.IsDir() || f.Name() == "layout.gohtml" || filepath.Ext(f.Name()) != ".gohtml" {
			continue
		}

		name := f.Name()
		tmpl := template.New(name)
		// Parse both the layout and the specific template from the embedded FS
		tmpl, err = tmpl.ParseFS(templateFS, "email/templates/layout.gohtml", "email/templates/"+name)
		if err != nil {
			log.Error().Err(err).Str("file", name).Msg("Failed to parse email template")
			continue
		}
		s.templates[name] = tmpl
	}

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

// SendWelcomeEmail sends a welcome email to a new host.
func (s *EmailService) SendWelcomeEmail(toEmail, hostName string) error {
	data := map[string]interface{}{
		"HostName":      hostName,
		"DashboardLink": fmt.Sprintf("%s/dashboard", s.cfg.FrontendURL),
		"Year":          time.Now().Year(),
	}

	html, err := s.renderTemplate("welcome.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "Welcome to QueueBuzz! 🚀", html)
	return nil
}

// SendAccountDeletionEmail sends a confirmation email when an account is deleted.
func (s *EmailService) SendAccountDeletionEmail(toEmail, hostName string) error {
	data := map[string]interface{}{
		"HostName": hostName,
		"Year":     time.Now().Year(),
	}

	html, err := s.renderTemplate("account_deletion.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "Account Deleted — We're sorry to see you go", html)
	return nil
}

// SendBuzzFallback sends an email notification when FCM delivery fails.
func (s *EmailService) SendBuzzFallback(toEmail, ticketNo, queueName, queueID string) error {
	data := map[string]interface{}{
		"TicketNo":  ticketNo,
		"QueueName": queueName,
		"Link":      fmt.Sprintf("%s/q/%s/waiting", s.cfg.FrontendURL, queueID),
		"Year":      time.Now().Year(),
	}

	html, err := s.renderTemplate("buzz_fallback.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "It's your turn! 🎉", html)
	return nil
}

// SendSessionRecovery sends a one-time queue recovery email to a guest.
func (s *EmailService) SendSessionRecovery(toEmail, queueName, link string) error {
	data := map[string]interface{}{
		"QueueName": queueName,
		"Link":      link,
		"Year":      time.Now().Year(),
	}

	html, err := s.renderTemplate("session_recovery.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "Recover your queue position — QueueBuzz", html)
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

// SendPurchaseConfirmation sends a confirmation email after a successful subscription purchase.
func (s *EmailService) SendPurchaseConfirmation(toEmail, planName, billingCycle string, amount int, currency string) error {
	// Format amount (stored as minor units, e.g. 49900 → ₹499.00)
	symbol := "₹"
	if currency == "USD" {
		symbol = "$"
	}
	formattedAmount := fmt.Sprintf("%s%.2f", symbol, float64(amount)/100.0)

	// Capitalize billing cycle for display
	displayCycle := "Monthly"
	if billingCycle == "yearly" {
		displayCycle = "Yearly"
	}

	data := map[string]interface{}{
		"PlanName":        planName,
		"BillingCycle":    displayCycle,
		"FormattedAmount": formattedAmount,
		"DashboardLink":   fmt.Sprintf("%s/dashboard", s.cfg.FrontendURL),
		"Year":            time.Now().Year(),
	}

	html, err := s.renderTemplate("purchase_confirmation.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "Payment Confirmed — QueueBuzz", html)
	return nil
}

// SendSubscriptionPending notifies the host that their payment is being processed.
func (s *EmailService) SendSubscriptionPending(toEmail, planName string) error {
	data := map[string]interface{}{
		"PlanName":      planName,
		"DashboardLink": fmt.Sprintf("%s/dashboard", s.cfg.FrontendURL),
		"Year":          time.Now().Year(),
	}

	html, err := s.renderTemplate("subscription_pending.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "Payment Processing — QueueBuzz", html)
	return nil
}

// SendSubscriptionRenewed notifies the host that their subscription was successfully renewed.
func (s *EmailService) SendSubscriptionRenewed(toEmail, planName, billingCycle string, amount int, currency string) error {
	symbol := "₹"
	if currency == "USD" {
		symbol = "$"
	}
	formattedAmount := fmt.Sprintf("%s%.2f", symbol, float64(amount)/100.0)

	displayCycle := "Monthly"
	if billingCycle == "yearly" {
		displayCycle = "Yearly"
	}

	data := map[string]interface{}{
		"PlanName":        planName,
		"BillingCycle":    displayCycle,
		"FormattedAmount": formattedAmount,
		"DashboardLink":   fmt.Sprintf("%s/dashboard", s.cfg.FrontendURL),
		"Year":            time.Now().Year(),
	}

	html, err := s.renderTemplate("subscription_renewed.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "Subscription Renewed — QueueBuzz", html)
	return nil
}

// SendPaymentFailed notifies the host that a recurring payment failed.
func (s *EmailService) SendPaymentFailed(toEmail, planName, billingCycle string) error {
	displayCycle := "Monthly"
	if billingCycle == "yearly" {
		displayCycle = "Yearly"
	}

	data := map[string]interface{}{
		"PlanName":      planName,
		"BillingCycle":  displayCycle,
		"DashboardLink": fmt.Sprintf("%s/dashboard", s.cfg.FrontendURL),
		"Year":          time.Now().Year(),
	}

	html, err := s.renderTemplate("payment_failed.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "Payment Failed — Action Required", html)
	return nil
}

// SendSubscriptionCancelled notifies the host that their subscription was cancelled.
func (s *EmailService) SendSubscriptionCancelled(toEmail, planName string) error {
	data := map[string]interface{}{
		"PlanName":    planName,
		"PricingLink": fmt.Sprintf("%s/pricing", s.cfg.FrontendURL),
		"Year":        time.Now().Year(),
	}

	html, err := s.renderTemplate("subscription_cancelled.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, "Subscription Cancelled — QueueBuzz", html)
	return nil
}

// SendRenewalReminder notifies hosts 2-4 days before their subscription renews.
func (s *EmailService) SendRenewalReminder(toEmail, planName string, renewalDate time.Time, amount int, currency string) error {
	symbol := "₹"
	if currency == "USD" {
		symbol = "$"
	}
	formattedAmount := fmt.Sprintf("%s%.2f", symbol, float64(amount)/100.0)

	daysUntil := int(math.Ceil(time.Until(renewalDate).Hours() / 24))
	if daysUntil < 1 {
		daysUntil = 1
	}

	data := map[string]interface{}{
		"PlanName":        planName,
		"FormattedAmount": formattedAmount,
		"RenewalDate":     renewalDate.Format("January 2, 2006"),
		"DaysUntil":       daysUntil,
		"DashboardLink":   fmt.Sprintf("%s/dashboard", s.cfg.FrontendURL),
		"Year":            time.Now().Year(),
	}

	html, err := s.renderTemplate("renewal_reminder.gohtml", data)
	if err != nil {
		return err
	}

	s.job.Dispatch(toEmail, fmt.Sprintf("Subscription renews in %d days — QueueBuzz", daysUntil), html)
	return nil
}

func (s *EmailService) renderTemplate(name string, data interface{}) (string, error) {
	tmpl, ok := s.templates[name]
	if !ok {
		return "", fmt.Errorf("template %s not found", name)
	}

	if m, ok := data.(map[string]interface{}); ok {
		if _, exists := m["FrontendURL"]; !exists {
			m["FrontendURL"] = s.cfg.FrontendURL
		}
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
