package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Delivery struct {
	ID             string
	NotificationID string
	Channel        string
	Attempt        int
	Severity       string
	Title          string
	Body           string
	UserID         string
	Recipient      string
}

func (s *Store) CreateNotification(ctx context.Context, userID, severity, title, body string, channels []string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO notifications(id,user_id,severity,title,body,created_at) VALUES(?,?,?,?,?,?)`,
		id, nullText(userID), severity, title, body, now); err != nil { return "", err }
	for _, channel := range channels {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO notification_deliveries
            (id,notification_id,channel,attempt,state,next_attempt_at) VALUES(?,?,?,0,'PENDING',?)`,
			uuid.NewString(), id, channel, now); err != nil { return "", err }
	}
	return id, nil
}

func (s *Store) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.notification_id,d.channel,d.attempt,n.severity,n.title,n.body,COALESCE(n.user_id,''),COALESCE(u.email,'')
		FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id LEFT JOIN users u ON u.id=n.user_id
        WHERE d.state='PENDING' AND d.next_attempt_at<=? ORDER BY d.next_attempt_at LIMIT ?`,
		now.UTC().Format(time.RFC3339Nano), limit)
	if err != nil { return nil, err }
	defer rows.Close()
	var result []Delivery
	for rows.Next() {
		var item Delivery
		if err := rows.Scan(&item.ID,&item.NotificationID,&item.Channel,&item.Attempt,&item.Severity,&item.Title,&item.Body,&item.UserID,&item.Recipient); err != nil { return nil, err }
		result = append(result,item)
	}
	return result, rows.Err()
}

func (s *Store) MarkDeliverySent(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE notification_deliveries SET state='SENT',last_error=NULL WHERE id=?", id)
	return err
}

func (s *Store) RescheduleDelivery(ctx context.Context, id string, attempt int, next time.Time, lastError string, dead bool) error {
	state := "PENDING"; if dead { state = "FAILED" }
	_, err := s.db.ExecContext(ctx, `UPDATE notification_deliveries SET attempt=?,state=?,next_attempt_at=?,last_error=? WHERE id=?`,
		attempt, state, next.UTC().Format(time.RFC3339Nano), lastError, id)
	return err
}
