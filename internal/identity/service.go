package identity

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/x4342/marketplace/internal/db"
)

var (
	ErrInvalid  = errors.New("invalid credentials")
	ErrConflict = errors.New("username already in use")
	ErrLimited  = errors.New("too many attempts")
)

type Service struct {
	pool  *pgxpool.Pool
	db    *db.Queries
	limit *limiter
	dummy []byte
}

type Enrollment struct {
	Session string
	Codes   []string
}

func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, db: db.New(pool), limit: newLimiter(), dummy: passwordHash(NewToken())}
}

func (s *Service) Register(ctx context.Context, username, password string) (Enrollment, error) {
	if username == "admin" || !Validate(username, password) {
		return Enrollment{}, ErrInvalid
	}
	release, ok := s.limit.admit("register", username)
	if !ok {
		return Enrollment{}, ErrLimited
	}
	defer release()

	encoded := passwordHash(password)
	var enrollment Enrollment
	err := s.inTx(ctx, func(q *db.Queries) error {
		userID, err := q.CreateUser(ctx, db.CreateUserParams{Username: username, PasswordHash: encoded})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "users_username_key" {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		enrollment, err = enroll(ctx, q, userID, "register")
		return err
	})
	return enrollment, err
}

func (s *Service) Login(ctx context.Context, username, password, previousToken string) (string, error) {
	if !Validate(username, password) {
		return "", ErrInvalid
	}
	release, ok := s.limit.admit("login", username)
	if !ok {
		return "", ErrLimited
	}
	defer release()

	user, err := s.db.UserCredential(ctx, username)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	encoded := user.PasswordHash
	if errors.Is(err, pgx.ErrNoRows) {
		encoded = s.dummy
	}
	if !passwordMatches(password, encoded) || !user.ID.Valid {
		return "", s.rejected(ctx, user.ID, "login_rejected")
	}

	token := NewToken()
	err = s.inTx(ctx, func(q *db.Queries) error {
		current, err := q.LockUser(ctx, user.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare(current, encoded) != 1 {
			return ErrInvalid
		}
		if ValidToken(previousToken) {
			if _, err := q.DeleteSession(ctx, db.DeleteSessionParams{UserID: user.ID, TokenHash: tokenHash(previousToken)}); err != nil {
				return err
			}
		}
		if err := q.CreateSession(ctx, db.CreateSessionParams{UserID: user.ID, TokenHash: tokenHash(token)}); err != nil {
			return err
		}
		return record(ctx, q, user.ID, "login")
	})
	return token, err
}

func (s *Service) Recover(ctx context.Context, username, code, password string) (Enrollment, error) {
	if !Validate(username, password) || !ValidToken(code) {
		return Enrollment{}, ErrInvalid
	}
	release, ok := s.limit.admit("recover", username)
	if !ok {
		return Enrollment{}, ErrLimited
	}
	defer release()

	user, err := s.db.UserCredential(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return Enrollment{}, s.rejected(ctx, pgtype.UUID{}, "recovery_rejected")
	}
	if err != nil {
		return Enrollment{}, err
	}

	var enrollment Enrollment
	err = s.inTx(ctx, func(q *db.Queries) error {
		if _, err := q.LockUser(ctx, user.ID); err != nil {
			return err
		}
		used, err := q.ConsumeRecoveryCode(ctx, db.ConsumeRecoveryCodeParams{UserID: user.ID, TokenHash: tokenHash(code)})
		if err != nil {
			return err
		}
		if used != 1 {
			return ErrInvalid
		}
		if err := q.UpdatePassword(ctx, db.UpdatePasswordParams{ID: user.ID, PasswordHash: passwordHash(password)}); err != nil {
			return err
		}
		if err := q.DeleteAllSessions(ctx, user.ID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, user.ID); err != nil {
			return err
		}
		enrollment, err = enroll(ctx, q, user.ID, "recover")
		return err
	})
	if errors.Is(err, ErrInvalid) {
		err = s.rejected(ctx, user.ID, "recovery_rejected")
	}
	return enrollment, err
}

func (s *Service) Active(ctx context.Context, token string) (db.ActiveSessionRow, error) {
	if !ValidToken(token) {
		return db.ActiveSessionRow{}, ErrInvalid
	}
	session, err := s.db.ActiveSession(ctx, tokenHash(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ActiveSessionRow{}, ErrInvalid
	}
	return session, err
}

func (s *Service) Logout(ctx context.Context, userID pgtype.UUID, token string) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		deleted, err := q.DeleteSession(ctx, db.DeleteSessionParams{UserID: userID, TokenHash: tokenHash(token)})
		if err != nil {
			return err
		}
		if deleted != 1 {
			return ErrInvalid
		}
		return record(ctx, q, userID, "logout")
	})
}

func Owns(userID, ownerID pgtype.UUID) bool {
	return userID.Valid && userID == ownerID
}

func enroll(ctx context.Context, queries *db.Queries, userID pgtype.UUID, event string) (Enrollment, error) {
	codes, err := issueRecoveryCodes(ctx, queries, userID)
	if err != nil {
		return Enrollment{}, err
	}
	result := Enrollment{Session: NewToken(), Codes: codes}
	if err := queries.CreateSession(ctx, db.CreateSessionParams{UserID: userID, TokenHash: tokenHash(result.Session)}); err != nil {
		return Enrollment{}, err
	}
	return result, record(ctx, queries, userID, event)
}

func record(ctx context.Context, q *db.Queries, id pgtype.UUID, action string) error {
	return q.RecordSecurityEvent(ctx, db.RecordSecurityEventParams{UserID: id, Action: action})
}

func (s *Service) rejected(ctx context.Context, id pgtype.UUID, action string) error {
	if err := record(ctx, s.db, id, action); err != nil {
		return err
	}
	return ErrInvalid
}

func (s *Service) inTx(ctx context.Context, fn func(*db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(s.db.WithTx(tx))
	})
}
