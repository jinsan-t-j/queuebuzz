package dto

type CreateQueueResponse struct {
	Name      string `json:"name"`
	JoinCode  string `json:"join_code"`
	Slug      string `json:"slug"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

type CheckSlugResponse struct {
	IsAvailable bool `json:"is_available"`
}
