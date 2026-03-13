package requests

type BroadcastRequest struct {
	Title string `json:"title" validate:"required,max=100"`
	Body  string `json:"body"  validate:"required,max=500"`
}
