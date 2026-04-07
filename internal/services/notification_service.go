package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"queuebuzz/internal/log"

	"golang.org/x/oauth2/google"
)

// --- NotificationSender interface ---

// NotificationSender abstracts push notification delivery.
// Swap implementations (Firebase, OneSignal, etc.) without touching handlers.
type NotificationSender interface {
	SendToUser(ctx context.Context, fcmToken, title, body string, data map[string]string) error
	SendToMultiple(ctx context.Context, fcmTokens []string, title, body string) error
}

// --- FirebaseSender implementation ---

type FirebaseSender struct {
	projectID  string
	credsJSON  []byte
	httpClient *http.Client
}

// NewFirebaseSender creates a singleton FirebaseSender.
// credentialsBase64 is the base64-encoded Firebase service account JSON.
func NewFirebaseSender(credentialsBase64 string) *FirebaseSender {
	credsJSON, err := base64.StdEncoding.DecodeString(credentialsBase64)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to decode Firebase credentials")
	}

	var creds struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(credsJSON, &creds); err != nil {
		log.Fatal().Err(err).Msg("Failed to parse Firebase credentials JSON")
	}

	return &FirebaseSender{
		projectID:  creds.ProjectID,
		credsJSON:  credsJSON,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type fcmMessage struct {
	Message fcmPayload `json:"message"`
}

type fcmPayload struct {
	Token        string            `json:"token"`
	Notification *fcmNotification  `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
	Android      *fcmAndroid       `json:"android,omitempty"`
	APNS         *fcmAPNS          `json:"apns,omitempty"`
}

type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type fcmAndroid struct {
	Priority string `json:"priority"`
}

type fcmAPNS struct {
	Headers map[string]string `json:"headers,omitempty"`
}

// SendToUser sends a push notification to a single device.
func (f *FirebaseSender) SendToUser(ctx context.Context, fcmToken, title, body string, data map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	msg := fcmMessage{
		Message: fcmPayload{
			Token: fcmToken,
			Notification: &fcmNotification{
				Title: title,
				Body:  body,
			},
			Data: data,
			Android: &fcmAndroid{
				Priority: "HIGH",
			},
			APNS: &fcmAPNS{
				Headers: map[string]string{
					"apns-priority": "10",
				},
			},
		},
	}

	return f.sendFCM(ctx, msg)
}

// SendToMultiple sends a push notification to multiple devices.
func (f *FirebaseSender) SendToMultiple(ctx context.Context, fcmTokens []string, title, body string) error {
	for _, token := range fcmTokens {
		if err := f.SendToUser(ctx, token, title, body, nil); err != nil {
			// Log error but continue sending to other tokens
			log.Error().
				Str("error", err.Error()).
				Msg("FCM delivery failed for a token")
		}
	}
	return nil
}

func (f *FirebaseSender) sendFCM(ctx context.Context, msg fcmMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal FCM payload: %w", err)
	}

	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", f.projectID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create FCM request: %w", err)
	}

	// Get OAuth2 access token for FCM v1 API
	accessToken, err := f.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get FCM access token: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send FCM request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Error().
			Int("status", resp.StatusCode).
			Msg("FCM delivery failed")
		return fmt.Errorf("FCM delivery failed with status %d", resp.StatusCode)
	}

	return nil
}

func (f *FirebaseSender) getAccessToken(ctx context.Context) (string, error) {
	creds, err := google.CredentialsFromJSONWithType(ctx, f.credsJSON, google.ServiceAccount, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return "", fmt.Errorf("failed to create credentials: %w", err)
	}

	token, err := creds.TokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}

	return token.AccessToken, nil
}
