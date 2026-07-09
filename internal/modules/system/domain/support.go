package domain

type SupportRequest struct {
	Name    string `json:"name" validate:"required,min=2,max=100"`
	Email   string `json:"email" validate:"required,email"`
	Subject string `json:"subject" validate:"required,min=3,max=200"`
	Message string `json:"message" validate:"required,min=10,max=5000"`
}
