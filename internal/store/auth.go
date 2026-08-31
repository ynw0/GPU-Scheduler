package store

import (
	"context"
	"database/sql"
	"time"

	"gpu-scheduler/internal/domain"
)

type UserRecord struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	Role         domain.Role
	TeamID       string
	FailedLogins int
	LockedUntil  *time.Time
	DisabledAt   *time.Time
}

type SessionRecord struct {
	IDHash     string
	UserID     string
	Username   string
	Email      string
	Role       domain.Role
	TeamID     string
	CSRFHash   string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

func (s *Store) CreateUser(ctx context.Context, user UserRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users
		(id,username,email,password_hash,role,team_id,created_at) VALUES(?,?,?,?,?,?,?)`,
		user.ID, user.Username, nullText(user.Email), user.PasswordHash, user.Role, nullText(user.TeamID), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) FindUserByUsername(ctx context.Context, username string) (UserRecord, error) {
	var user UserRecord
	var teamID, lockedUntil, disabledAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,username,COALESCE(email,''),password_hash,role,team_id,failed_logins,locked_until,disabled_at
        FROM users WHERE username=?`, username).Scan(
		&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.Role, &teamID,
		&user.FailedLogins, &lockedUntil, &disabledAt)
	if err != nil {
		return UserRecord{}, err
	}
	user.TeamID = teamID.String
	user.LockedUntil = parseOptionalTime(lockedUntil)
	user.DisabledAt = parseOptionalTime(disabledAt)
	return user, nil
}

func (s *Store) RecordFailedLogin(ctx context.Context, userID string, maxAttempts int, lockDuration time.Duration) error {
	var attempts int
	if err := s.db.QueryRowContext(ctx, "SELECT failed_logins FROM users WHERE id=?", userID).Scan(&attempts); err != nil {
		return err
	}
	attempts++
	var locked any
	if attempts >= maxAttempts {
		locked = time.Now().UTC().Add(lockDuration).Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, "UPDATE users SET failed_logins=?, locked_until=? WHERE id=?", attempts, locked, userID)
	return err
}

func (s *Store) ClearFailedLogins(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE users SET failed_logins=0, locked_until=NULL WHERE id=?", userID)
	return err
}

func (s *Store) CreateSession(ctx context.Context, session SessionRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions
        (id_hash,user_id,csrf_hash,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?)`,
		session.IDHash, session.UserID, session.CSRFHash,
		session.CreatedAt.Format(time.RFC3339Nano), session.LastSeenAt.Format(time.RFC3339Nano), session.ExpiresAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) FindSession(ctx context.Context, idHash string) (SessionRecord, error) {
	var session SessionRecord
	var teamID sql.NullString
	var createdAt, lastSeenAt, expiresAt string
	err := s.db.QueryRowContext(ctx, `SELECT s.id_hash,s.user_id,u.username,COALESCE(u.email,''),u.role,u.team_id,s.csrf_hash,s.created_at,s.last_seen_at,s.expires_at
        FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id_hash=? AND u.disabled_at IS NULL`, idHash).Scan(
		&session.IDHash, &session.UserID, &session.Username, &session.Email, &session.Role, &teamID,
		&session.CSRFHash, &createdAt, &lastSeenAt, &expiresAt)
	if err != nil {
		return SessionRecord{}, err
	}
	session.TeamID = teamID.String
	session.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	session.LastSeenAt, _ = time.Parse(time.RFC3339Nano, lastSeenAt)
	session.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)
	return session, nil
}

func (s *Store) TouchSession(ctx context.Context, idHash string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET last_seen_at=? WHERE id_hash=?", now.Format(time.RFC3339Nano), idHash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id_hash=?", idHash)
	return err
}

func nullText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}
	return &parsed
}
