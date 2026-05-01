package email

/**
 * Provider defines the interface for email sending implementations.
 * We support Brevo for production and Mailpit for local development.
 */
type Provider interface {
	Send(to, subject, html string) error
}
