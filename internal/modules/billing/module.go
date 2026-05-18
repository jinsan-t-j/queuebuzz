package billing

import (
	"queuebuzz/internal/middlewares"
	authservice "queuebuzz/internal/modules/auth/service"
	"queuebuzz/internal/modules/billing/http"
	"queuebuzz/internal/modules/billing/jobs"
	"queuebuzz/internal/modules/billing/service"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	Handler     *http.Handler
	Service     *service.BillingService
	AuthService *authservice.AuthService
	ReminderJob *jobs.RenewalReminderJob
}

func New(handler *http.Handler, service *service.BillingService, authSvc *authservice.AuthService, reminderJob *jobs.RenewalReminderJob) *Module {
	return &Module{
		Handler:     handler,
		Service:     service,
		AuthService: authSvc,
		ReminderJob: reminderJob,
	}
}

func (m *Module) RegisterRoutes(router fiber.Router) {
	group := router.Group("/billing")

	group.Get("/plans", m.Handler.ListPlans)
	group.Post("/webhook", m.Handler.HandleWebhook)

	auth := middlewares.HostAuthMiddleware(m.AuthService)
	optionalAuth := middlewares.OptionalAuthMiddleware(m.AuthService)
	group.Post("/checkout", auth, m.Handler.GetCheckoutURL)
	group.Get("/current-plan", optionalAuth, m.Handler.GetCurrentPlan)

	// Subscription management (authenticated)
	group.Get("/subscription", auth, m.Handler.GetSubscription)
	group.Post("/subscription/cancel", auth, m.Handler.CancelSubscription)
	group.Post("/subscription/update-payment-method", auth, m.Handler.UpdatePaymentMethod)

	group.Get("/success", m.Handler.PaymentSuccess)
	group.Get("/cancel", m.Handler.PaymentCancel)
}
