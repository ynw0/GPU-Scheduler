package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"time"

	"gpu-scheduler/internal/config"
	"gpu-scheduler/internal/store"
)

var retryDelays = []time.Duration{time.Minute, 5*time.Minute, 30*time.Minute, 2*time.Hour, 6*time.Hour}

type Worker struct {
	store *store.Store
	cfg config.Notifications
	http *http.Client
}

func NewWorker(database *store.Store, cfg config.Notifications, timeout time.Duration) *Worker {
	return &Worker{store: database, cfg: cfg, http: &http.Client{Timeout: timeout}}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		w.process(ctx)
		select { case <-ctx.Done(): return; case <-ticker.C: }
	}
}

func (w *Worker) Publish(ctx context.Context, userID, severity, title, body string) (string, error) {
	var channels []string
	if w.cfg.SMTP.Enabled { channels = append(channels, "email") }
	if w.cfg.Webhook.Enabled { channels = append(channels, "webhook") }
	return w.store.CreateNotification(ctx, userID, severity, title, body, channels)
}

func (w *Worker) process(ctx context.Context) {
	deliveries, err := w.store.DueDeliveries(ctx, time.Now().UTC(), 50)
	if err != nil { return }
	for _, delivery := range deliveries {
		err := w.deliver(ctx, delivery)
		if err == nil { _ = w.store.MarkDeliverySent(ctx, delivery.ID); continue }
		attempt := delivery.Attempt + 1
		dead := attempt >= len(retryDelays)
		delay := retryDelays[len(retryDelays)-1]
		if !dead { delay = retryDelays[attempt-1] }
		_ = w.store.RescheduleDelivery(ctx, delivery.ID, attempt, time.Now().UTC().Add(delay), err.Error(), dead)
	}
}

func (w *Worker) deliver(ctx context.Context, delivery store.Delivery) error {
	switch delivery.Channel {
	case "webhook": return w.sendWebhook(ctx, delivery)
	case "email": return w.sendEmail(delivery)
	default: return fmt.Errorf("unknown notification channel %s", delivery.Channel)
	}
}

func (w *Worker) sendWebhook(ctx context.Context, delivery store.Delivery) error {
	payload, _ := json.Marshal(map[string]any{
		"eventId": delivery.NotificationID, "severity": delivery.Severity,
		"title": delivery.Title, "body": delivery.Body, "userId": delivery.UserID,
	})
	mac := hmac.New(sha256.New, []byte(w.cfg.Webhook.Secret)); _, _ = mac.Write(payload)
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.Webhook.URL, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GPU-Scheduler-Event", delivery.NotificationID)
	request.Header.Set("X-GPU-Scheduler-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response, err := w.http.Do(request)
	if err != nil { return err }
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 { return fmt.Errorf("webhook returned %s", response.Status) }
	return nil
}

func (w *Worker) sendEmail(delivery store.Delivery) error {
	connection, err := tls.Dial("tcp", w.cfg.SMTP.Address, &tls.Config{ServerName: w.cfg.SMTP.ServerName, MinVersion: tls.VersionTLS12})
	if err != nil { return err }
	defer connection.Close()
	client, err := smtp.NewClient(connection, w.cfg.SMTP.ServerName)
	if err != nil { return err }
	defer client.Close()
	if w.cfg.SMTP.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", w.cfg.SMTP.Username, w.cfg.SMTP.Password, w.cfg.SMTP.ServerName)); err != nil { return err }
	}
	if err := client.Mail(w.cfg.SMTP.From); err != nil { return err }
	if delivery.Recipient == "" { return fmt.Errorf("notification has no recipient email mapping") }
	if err := client.Rcpt(delivery.Recipient); err != nil { return err }
	writer, err := client.Data(); if err != nil { return err }
	message := "From: "+w.cfg.SMTP.From+"\r\nTo: "+delivery.Recipient+"\r\nSubject: "+delivery.Title+"\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n"+delivery.Body
	_, err = writer.Write([]byte(message)); closeErr := writer.Close()
	if err != nil { return err }; return closeErr
}
