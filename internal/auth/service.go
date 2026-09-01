package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/store"
)

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrWrongPassword = errors.New("当前密码不正确")

type Service struct {
	store       *store.Store
	idleTimeout time.Duration
	absoluteTTL time.Duration
	minPassword int
}

type Identity struct {
	UserID   string      `json:"id"`
	Username string      `json:"username"`
	Role     domain.Role `json:"role"`
	TeamID   string      `json:"teamId,omitempty"`
}

type LoginResult struct {
	Identity     Identity
	SessionToken string
	CSRFToken    string
	ExpiresAt    time.Time
}

func NewService(store *store.Store, idleTimeout, absoluteTTL time.Duration, minPassword int) *Service {
	return &Service{store: store, idleTimeout: idleTimeout, absoluteTTL: absoluteTTL, minPassword: minPassword}
}

func (s *Service) passwordTooShort() error {
	return fmt.Errorf("密码短于配置下限（%d 位）", s.minPassword)
}

func (s *Service) CreateUser(ctx context.Context, username, email, password string, role domain.Role, teamID string) (Identity, error) {
	if len(password) < s.minPassword {
		return Identity{}, s.passwordTooShort()
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Identity{}, err
	}
	id := uuid.NewString()
	if err := s.store.CreateUser(ctx, store.UserRecord{
		ID: id, Username: username, Email: email, PasswordHash: hash, Role: role, TeamID: teamID,
	}); err != nil {
		return Identity{}, err
	}
	return Identity{UserID: id, Username: username, Role: role, TeamID: teamID}, nil
}

func (s *Service) BootstrapAdmin(ctx context.Context, username, email, password string) error {
	if len(password) < s.minPassword {
		return s.passwordTooShort()
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.store.CreateUser(ctx, store.UserRecord{
		ID: uuid.NewString(), Username: username, Email: email, PasswordHash: hash, Role: domain.RoleAdmin,
	})
}

func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	user, err := s.store.FindUserByUsername(ctx, username)
	if err != nil || user.DisabledAt != nil || (user.LockedUntil != nil && user.LockedUntil.After(time.Now().UTC())) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if !VerifyPassword(user.PasswordHash, password) {
		_ = s.store.RecordFailedLogin(ctx, user.ID, 5, 15*time.Minute)
		return LoginResult{}, ErrInvalidCredentials
	}
	if err := s.store.ClearFailedLogins(ctx, user.ID); err != nil {
		return LoginResult{}, err
	}
	sessionToken, err := randomToken(32)
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken, err := randomToken(24)
	if err != nil {
		return LoginResult{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(s.absoluteTTL)
	if err := s.store.CreateSession(ctx, store.SessionRecord{
		IDHash: hashToken(sessionToken), UserID: user.ID, CSRFHash: hashToken(csrfToken),
		CreatedAt: now, LastSeenAt: now, ExpiresAt: expiresAt,
	}); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		Identity:     Identity{UserID: user.ID, Username: user.Username, Role: user.Role, TeamID: user.TeamID},
		SessionToken: sessionToken, CSRFToken: csrfToken, ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) Authenticate(ctx context.Context, sessionToken string) (Identity, error) {
	record, err := s.store.FindSession(ctx, hashToken(sessionToken))
	if err != nil {
		return Identity{}, ErrInvalidCredentials
	}
	now := time.Now().UTC()
	if now.After(record.ExpiresAt) || now.Sub(record.LastSeenAt) > s.idleTimeout {
		_ = s.store.DeleteSession(ctx, record.IDHash)
		return Identity{}, ErrInvalidCredentials
	}
	if err := s.store.TouchSession(ctx, record.IDHash, now); err != nil {
		return Identity{}, err
	}
	return Identity{UserID: record.UserID, Username: record.Username, Role: record.Role, TeamID: record.TeamID}, nil
}

func (s *Service) VerifyCSRF(ctx context.Context, sessionToken, csrfToken string) bool {
	record, err := s.store.FindSession(ctx, hashToken(sessionToken))
	return err == nil && record.CSRFHash == hashToken(csrfToken)
}

func (s *Service) Logout(ctx context.Context, sessionToken string) error {
	return s.store.DeleteSession(ctx, hashToken(sessionToken))
}

func (s *Service) ChangeOwnPassword(ctx context.Context, userID, current, next string) error {
	if len(next) < s.minPassword {
		return s.passwordTooShort()
	}
	user, err := s.store.FindUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !VerifyPassword(user.PasswordHash, current) {
		return ErrWrongPassword
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.store.UpdatePasswordHash(ctx, userID, hash)
}

func randomToken(length int) (string, error) {
	value := make([]byte, length)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
