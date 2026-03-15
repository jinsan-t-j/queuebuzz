package notification

import (
	notificationhttp "queuebuzz/internal/modules/notification/http"
)

type Module struct {
	Handler *notificationhttp.Handler
}

func New(handler *notificationhttp.Handler) *Module {
	return &Module{Handler: handler}
}
