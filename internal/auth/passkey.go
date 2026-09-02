package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/nexryai/rosmarinus/internal/account"
)

type CeremonyType string

const (
	CeremonyInitialRegistration CeremonyType = "initial_registration"
	CeremonyLogin               CeremonyType = "login"
)

var (
	ErrRegistrationClosed = errors.New("initial registration is closed")
	ErrCeremonyNotFound   = errors.New("WebAuthn ceremony not found or expired")
)

type User struct {
	Account     account.Account
	Credentials []webauthn.Credential
}

func (u *User) WebAuthnID() []byte          { return append([]byte(nil), u.Account.WebAuthnID...) }
func (u *User) WebAuthnName() string        { return u.Account.Username }
func (u *User) WebAuthnDisplayName() string { return u.Account.DisplayName }
func (u *User) WebAuthnCredentials() []webauthn.Credential {
	return append([]webauthn.Credential(nil), u.Credentials...)
}

type AccountStore interface {
	ReserveInitial(context.Context, account.Account, time.Time) (*User, error)
	FindPendingByID(context.Context, string, time.Time) (*User, error)
	ActivateInitial(context.Context, string, webauthn.Credential, time.Time) error
	DeletePending(context.Context, string) error
	FindActiveByUserHandle(context.Context, []byte) (*User, error)
	UpdateCredential(context.Context, string, webauthn.Credential, webauthn.Credential, time.Time) error
}

type Ceremony struct {
	ID        string
	Type      CeremonyType
	AccountID string
	Session   webauthn.SessionData
	CreatedAt time.Time
	ExpiresAt time.Time
}

type CeremonyStore interface {
	Create(context.Context, Ceremony) error
	Consume(context.Context, string, CeremonyType, time.Time) (*Ceremony, error)
}

type PasskeyService struct {
	webauthn    *webauthn.WebAuthn
	accounts    AccountStore
	ceremonies  CeremonyStore
	sessions    *SessionManager
	now         func() time.Time
	random      func(int) (string, error)
	ceremonyTTL time.Duration
}

type CeremonyOptions struct {
	CeremonyID string `json:"ceremony_id"`
	PublicKey  any    `json:"public_key"`
}

func NewPasskeyService(cfg *webauthn.Config, accounts AccountStore, ceremonies CeremonyStore, sessions *SessionManager, ceremonyTTL time.Duration) (*PasskeyService, error) {
	wa, err := webauthn.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("configure WebAuthn: %w", err)
	}
	if ceremonyTTL <= 0 {
		ceremonyTTL = 5 * time.Minute
	}
	return &PasskeyService{
		webauthn: wa, accounts: accounts, ceremonies: ceremonies, sessions: sessions,
		now: func() time.Time { return time.Now().UTC() }, random: randomToken, ceremonyTTL: ceremonyTTL,
	}, nil
}

func (s *PasskeyService) BeginInitialRegistration(ctx context.Context, username, displayName string) (CeremonyOptions, error) {
	if s == nil || s.accounts == nil || s.ceremonies == nil {
		return CeremonyOptions{}, fmt.Errorf("passkey service is not configured")
	}
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	if username == "" || len(username) > 64 {
		return CeremonyOptions{}, fmt.Errorf("username must contain 1 to 64 characters")
	}
	if displayName == "" {
		displayName = username
	}
	if len(displayName) > 128 {
		return CeremonyOptions{}, fmt.Errorf("display name must not exceed 128 characters")
	}
	accountID, err := s.random(18)
	if err != nil {
		return CeremonyOptions{}, err
	}
	userHandle, err := randomBytes(32)
	if err != nil {
		return CeremonyOptions{}, err
	}
	now := s.now()
	user, err := s.accounts.ReserveInitial(ctx, account.Account{
		ID: accountID, Username: username, DisplayName: displayName,
		WebAuthnID: userHandle, Status: account.StatusPending, CreatedAt: now, UpdatedAt: now,
	}, now.Add(s.ceremonyTTL))
	if err != nil {
		return CeremonyOptions{}, err
	}
	creation, session, err := s.webauthn.BeginMediatedRegistration(user, protocol.MediationDefault,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	)
	if err != nil {
		_ = s.accounts.DeletePending(ctx, accountID)
		return CeremonyOptions{}, fmt.Errorf("begin passkey registration: %w", err)
	}
	ceremonyID, err := s.random(18)
	if err != nil {
		_ = s.accounts.DeletePending(ctx, accountID)
		return CeremonyOptions{}, err
	}
	if err := s.ceremonies.Create(ctx, Ceremony{
		ID: ceremonyID, Type: CeremonyInitialRegistration, AccountID: accountID,
		Session: *session, CreatedAt: now, ExpiresAt: now.Add(s.ceremonyTTL),
	}); err != nil {
		_ = s.accounts.DeletePending(ctx, accountID)
		return CeremonyOptions{}, fmt.Errorf("store registration ceremony: %w", err)
	}
	return CeremonyOptions{CeremonyID: ceremonyID, PublicKey: creation}, nil
}

func (s *PasskeyService) FinishInitialRegistration(ctx context.Context, ceremonyID string, response *http.Request) (SessionCredentials, error) {
	ceremony, err := s.consume(ctx, ceremonyID, CeremonyInitialRegistration)
	if err != nil {
		return SessionCredentials{}, err
	}
	user, err := s.accounts.FindPendingByID(ctx, ceremony.AccountID, s.now())
	if err != nil || user == nil {
		return SessionCredentials{}, ErrCeremonyNotFound
	}
	credential, err := s.webauthn.FinishRegistration(user, ceremony.Session, response)
	if err != nil {
		_ = s.accounts.DeletePending(ctx, ceremony.AccountID)
		return SessionCredentials{}, fmt.Errorf("verify passkey registration: %w", err)
	}
	if err := s.accounts.ActivateInitial(ctx, ceremony.AccountID, *credential, s.now()); err != nil {
		return SessionCredentials{}, fmt.Errorf("activate initial account: %w", err)
	}
	return s.sessions.Create(ctx, ceremony.AccountID)
}

func (s *PasskeyService) BeginLogin(ctx context.Context) (CeremonyOptions, error) {
	assertion, session, err := s.webauthn.BeginDiscoverableMediatedLogin(protocol.MediationDefault,
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		return CeremonyOptions{}, fmt.Errorf("begin passkey login: %w", err)
	}
	ceremonyID, err := s.random(18)
	if err != nil {
		return CeremonyOptions{}, err
	}
	now := s.now()
	if err := s.ceremonies.Create(ctx, Ceremony{
		ID: ceremonyID, Type: CeremonyLogin, Session: *session,
		CreatedAt: now, ExpiresAt: now.Add(s.ceremonyTTL),
	}); err != nil {
		return CeremonyOptions{}, fmt.Errorf("store login ceremony: %w", err)
	}
	return CeremonyOptions{CeremonyID: ceremonyID, PublicKey: assertion}, nil
}

func (s *PasskeyService) FinishLogin(ctx context.Context, ceremonyID string, response *http.Request) (SessionCredentials, error) {
	ceremony, err := s.consume(ctx, ceremonyID, CeremonyLogin)
	if err != nil {
		return SessionCredentials{}, err
	}
	var found *User
	loadedUser, credential, err := s.webauthn.FinishPasskeyLogin(func(_, userHandle []byte) (webauthn.User, error) {
		user, err := s.accounts.FindActiveByUserHandle(ctx, userHandle)
		if err != nil {
			return nil, err
		}
		if user == nil {
			return nil, ErrUnauthenticated
		}
		found = user
		return user, nil
	}, ceremony.Session, response)
	if err != nil {
		return SessionCredentials{}, fmt.Errorf("verify passkey login: %w", err)
	}
	user, ok := loadedUser.(*User)
	if !ok || found == nil || user.Account.ID != found.Account.ID {
		return SessionCredentials{}, ErrUnauthenticated
	}
	var previous *webauthn.Credential
	for i := range found.Credentials {
		if string(found.Credentials[i].ID) == string(credential.ID) {
			copy := found.Credentials[i]
			previous = &copy
			break
		}
	}
	if previous == nil {
		return SessionCredentials{}, ErrUnauthenticated
	}
	if err := s.accounts.UpdateCredential(ctx, user.Account.ID, *previous, *credential, s.now()); err != nil {
		return SessionCredentials{}, fmt.Errorf("update passkey credential: %w", err)
	}
	return s.sessions.Create(ctx, user.Account.ID)
}

func (s *PasskeyService) consume(ctx context.Context, ceremonyID string, ceremonyType CeremonyType) (*Ceremony, error) {
	ceremonyID = strings.TrimSpace(ceremonyID)
	if ceremonyID == "" {
		return nil, ErrCeremonyNotFound
	}
	ceremony, err := s.ceremonies.Consume(ctx, ceremonyID, ceremonyType, s.now())
	if err != nil {
		return nil, err
	}
	if ceremony == nil {
		return nil, ErrCeremonyNotFound
	}
	return ceremony, nil
}
