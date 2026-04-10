package firebase

import (
	"context"
	"encoding/base64"

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

// NewSender creates a singleton Sender initializing the Admin SDK.
func NewSender(credentialsBase64 string) *Sender {
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

// SendToUser sends a push notification to a single device token.
func (f *Sender) SendToUser(ctx context.Context, fcmToken, title, body string, data map[string]string) error {
	if fcmToken == "" {
		return nil
	}

	msg := &messaging.Message{
		Token: fcmToken,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
		// Ensure high priority for time-sensitive queue updates
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
		APNS: &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					ContentAvailable: true,
				},
			},
		},
	}

	_, err := f.client.Send(ctx, msg)
	if err != nil {
		log.Error().Err(err).Str("token", fcmToken).Msg("FCM: failed to send notification")
		return err
	}

	return nil
}

// SendToMultiple sends a push notification to multiple device tokens using multicast.
func (f *Sender) SendToMultiple(ctx context.Context, fcmTokens []string, title, body string, data map[string]string) error {
	if len(fcmTokens) == 0 {
		return nil
	}

	msg := &messaging.MulticastMessage{
		Tokens: fcmTokens,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
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
