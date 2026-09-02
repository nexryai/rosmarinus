package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/account"
)

const DefaultSessionCookieName = "rosmarinus_session"

var ErrUnauthenticated = errors.New("session is not authenticated")

type Session struct {
	ID        string
	AccountID string
	TokenHash string
	CSRFToken string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type SessionRepository interface {
	Create(context.Context, Session) error
	FindActiveByTokenHash(context.Context, string, time.Time) (*Session, error)
	RevokeByTokenHash(context.Context, string, time.Time) error
}

type AccountRepository interface {
	FindByID(context.Context, string) (*account.Account, error)
}

type SessionCredentials struct {
	Token     string
	CSRFToken string
	ExpiresAt time.Time
}

type SessionManager struct {
	sessions SessionRepository
	accounts AccountRepository
	cookie   string
	ttl      time.Duration
	secure   bool
	now      func() time.Time
	random   func(int) (string, error)
}

func NewSessionManager(sessions SessionRepository, accounts AccountRepository, cookieName string, ttl time.Duration, secure bool) *SessionManager {
	if strings.TrimSpace(cookieName) == "" {
		cookieName = DefaultSessionCookieName
	}
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	return &SessionManager{
		sessions: sessions,
		accounts: accounts,
		cookie:   cookieName,
		ttl:      ttl,
		secure:   secure,
		now:      func() time.Time { return time.Now().UTC() },
		random:   randomToken,
	}
}

func (m *SessionManager) Create(ctx context.Context, accountID string) (SessionCredentials, error) {
	if m == nil || m.sessions == nil || m.accounts == nil {
		return SessionCredentials{}, fmt.Errorf("session manager is not configured")
	}
	accountRecord, err := m.accounts.FindByID(ctx, strings.TrimSpace(accountID))
	if err != nil {
		return SessionCredentials{}, fmt.Errorf("load session account: %w", err)
	}
	if !accountRecord.IsActive() {
		return SessionCredentials{}, ErrUnauthenticated
	}
	token, err := m.random(32)
	if err != nil {
		return SessionCredentials{}, fmt.Errorf("generate session token: %w", err)
	}
	csrfToken, err := m.random(32)
	if err != nil {
		return SessionCredentials{}, fmt.Errorf("generate CSRF token: %w", err)
	}
	sessionID, err := m.random(18)
	if err != nil {
		return SessionCredentials{}, fmt.Errorf("generate session id: %w", err)
	}
	now := m.now()
	expiresAt := now.Add(m.ttl)
	if err := m.sessions.Create(ctx, Session{
		ID:        sessionID,
		AccountID: accountRecord.ID,
		TokenHash: tokenHash(token),
		CSRFToken: csrfToken,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}); err != nil {
		return SessionCredentials{}, fmt.Errorf("store session: %w", err)
	}
	return SessionCredentials{Token: token, CSRFToken: csrfToken, ExpiresAt: expiresAt}, nil
}

func (m *SessionManager) Authenticate(r *http.Request) (string, string, error) {
	if m == nil || m.sessions == nil || m.accounts == nil || r == nil {
		return "", "", ErrUnauthenticated
	}
	cookie, err := r.Cookie(m.cookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", "", ErrUnauthenticated
	}
	session, err := m.sessions.FindActiveByTokenHash(r.Context(), tokenHash(cookie.Value), m.now())
	if err != nil {
		return "", "", fmt.Errorf("load session: %w", err)
	}
	if session == nil || session.RevokedAt != nil || !session.ExpiresAt.After(m.now()) {
		return "", "", ErrUnauthenticated
	}
	accountRecord, err := m.accounts.FindByID(r.Context(), session.AccountID)
	if err != nil {
		return "", "", fmt.Errorf("load session account: %w", err)
	}
	if !accountRecord.IsActive() {
		return "", "", ErrUnauthenticated
	}
	return accountRecord.ID, session.CSRFToken, nil
}

func (m *SessionManager) Revoke(r *http.Request) error {
	if m == nil || m.sessions == nil || r == nil {
		return ErrUnauthenticated
	}
	cookie, err := r.Cookie(m.cookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return ErrUnauthenticated
	}
	if err := m.sessions.RevokeByTokenHash(r.Context(), tokenHash(cookie.Value), m.now()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (m *SessionManager) SetCookie(w http.ResponseWriter, credentials SessionCredentials) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookie,
		Value:    credentials.Token,
		Path:     "/",
		Expires:  credentials.ExpiresAt,
		MaxAge:   maxAge(m.now(), credentials.ExpiresAt),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *SessionManager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(size int) (string, error) {
	value, err := randomBytes(size)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	return value, nil
}

func maxAge(now, expiresAt time.Time) int {
	seconds := int(expiresAt.Sub(now).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}
