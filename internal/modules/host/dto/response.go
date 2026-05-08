package dto

type GetMeResponse struct {
	ID              string `json:"id"`
	PublicID        string `json:"public_id"`
	Name            string `json:"name"`
	BusinessName    string `json:"business_name"`
	Address         string `json:"address"`
	Email           string `json:"email"`
	Tier            string `json:"tier"`
	Avatar          string `json:"avatar,omitempty"`
	ProfileImageURL string `json:"profile_image_url,omitempty"`
	BannerImageURL  string `json:"banner_image_url,omitempty"`
}
