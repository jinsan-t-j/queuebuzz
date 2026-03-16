package dto

type GetMeResponse struct {
	ID       string `json:"id"`
	PublicID string `json:"public_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Tier     string `json:"tier"`
	Avatar   string `json:"avatar,omitempty"`
}
