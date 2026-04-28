package firebase

import (
	"context"
	"encoding/base64"
	"strings"

	"queuebuzz/internal/log"

	fbase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

// NotificationSender abstracts push notification delivery.
type NotificationSender interface {
	SendToUser(ctx context.Context, fcmToken, title, body string, data map[string]string) error
	SendToMultiple(ctx context.Context, fcmTokens []string, title, body string, data map[string]string) error
}

// Sender implements NotificationSender using the official Firebase Admin SDK.
type Sender struct {
	app    *fbase.App
	client *messaging.Client
}

// NewSender initializes the Firebase Admin SDK.
func NewSender(credentialsBase64 string) NotificationSender {
	credsJSON, err := base64.StdEncoding.DecodeString(credentialsBase64)
	if err != nil {
		log.Fatal().Err(err).Msg("FCM: failed to decode credentials from base64")
	}

	opt := option.WithCredentialsJSON(credsJSON) //nolint:staticcheck

	// We use Background context for initialization as this app lives as long as the process
	app, err := fbase.NewApp(context.Background(), nil, opt)
	if err != nil {
		log.Fatal().Err(err).Msg("FCM: failed to initialize Firebase App")
	}

	client, err := app.Messaging(context.Background())
	if err != nil {
		log.Fatal().Err(err).Msg("FCM: failed to initialize Messaging client")
	}

	log.Info().Msg("FCM: Firebase Admin SDK initialized successfully")

	return &Sender{
		app:    app,
		client: client,
	}
}

func buildNotificationData(data map[string]string, title, body string) map[string]string {
	payload := make(map[string]string, len(data)+2)
	for key, value := range data {
		payload[key] = value
	}

	if payload["title"] == "" {
		payload["title"] = title
	}

	if payload["body"] == "" {
		payload["body"] = body
	}

	return payload
}

func buildWebpushConfig(data map[string]string) *messaging.WebpushConfig {
	headers := map[string]string{
		"TTL":     "60",
		"Urgency": "high",
	}

	config := &messaging.WebpushConfig{
		Headers: headers,
		Notification: &messaging.WebpushNotification{
			Icon:  "/icons/notification-icon.png",
			Badge: "/icons/badge-icon.png",
			Tag:   "queue-buzz",
		},
	}

	if link := data["link"]; link != "" {
		config.FCMOptions = &messaging.WebpushFCMOptions{
			Link: link,
		}
	}

	return config
}

func buildAndroidConfig() *messaging.AndroidConfig {
	return &messaging.AndroidConfig{
		Priority: "high",
		Notification: &messaging.AndroidNotification{
			Sound:               "default",
			VibrateTimingMillis: []int64{0, 200, 100, 300},
		},
	}
}

func buildAPNSConfig() *messaging.APNSConfig {
	return &messaging.APNSConfig{
		Payload: &messaging.APNSPayload{
			Aps: &messaging.Aps{
				ContentAvailable: true,
				Sound:            "default",
			},
		},
	}
}

// SendToUser sends a push notification to a single device token.
func (f *Sender) SendToUser(ctx context.Context, fcmToken, title, body string, data map[string]string) error {
	if fcmToken == "" {
		return nil
	}

	payloadData := buildNotificationData(data, title, body)

	msg := &messaging.Message{
		Token: fcmToken,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data:    payloadData,
		Android: buildAndroidConfig(),
		Webpush: buildWebpushConfig(payloadData),
		APNS:    buildAPNSConfig(),
	}

	name, err := f.client.Send(ctx, msg)
	if err != nil {
		log.Error().Err(err).Str("token", fcmToken).Msgf("FCM: failed to send notification. Raw error: %+v", err)
		return err
	}

	log.Info().Str("token", fcmToken).Str("name", name).Msg("FCM: notification sent successfully")

	return nil
}

func IsTokenInvalid(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "requested entity was not found") ||
		strings.Contains(message, "registration-token-not-registered") ||
		strings.Contains(message, "registration token is not registered") ||
		strings.Contains(message, "device unregistered")
}

// SendToMultiple sends a push notification to multiple device tokens using multicast.
func (f *Sender) SendToMultiple(ctx context.Context, fcmTokens []string, title, body string, data map[string]string) error {
	if len(fcmTokens) == 0 {
		return nil
	}

	payloadData := buildNotificationData(data, title, body)

	msg := &messaging.MulticastMessage{
		Tokens: fcmTokens,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data:    payloadData,
		Android: buildAndroidConfig(),
		Webpush: buildWebpushConfig(payloadData),
		APNS:    buildAPNSConfig(),
	}

	res, err := f.client.SendEachForMulticast(ctx, msg)
	if err != nil {
		log.Error().Err(err).Msg("FCM: multicast send failed")
		return err
	}

	if res.FailureCount > 0 {
		log.Warn().Int("failure_count", res.FailureCount).Msg("FCM: some notifications in multicast failed")
	}

	return nil
}
