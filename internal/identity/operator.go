package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/x4342/marketplace/internal/authz"
	"github.com/x4342/marketplace/internal/db"
)

var (
	ErrLastAdministrator = errors.New("Keep at least one administrator. Assign a second administrator before removing this role.")
	ErrRoleChanged       = errors.New("The account role changed. Open the current role list and try again.")
	ErrBootstrapComplete = errors.New("administrator bootstrap is already reserved or complete")
	ErrAdminNameInUse    = errors.New("admin username already exists; use explicit verified claim")
	ErrOperator          = errors.New("administrator or operator action is unavailable")
)

// ConfirmStaff authenticates the current account again for one sensitive operation.
func (s *Service) ConfirmStaff(ctx context.Context, actor db.ActiveSessionRow, password string) error {
	if !authz.Allows(actor.Role, authz.Staff) || !Validate(actor.Username, password) {
		return ErrInvalid
	}
	release, ok := s.limit.admit("staff_reauth", actor.Username)
	if !ok {
		return ErrLimited
	}
	defer release()
	credential, err := s.db.UserCredential(ctx, actor.Username)
	if err != nil {
		return err
	}
	if credential.ID != actor.ID || !passwordMatches(password, credential.PasswordHash) {
		return s.rejected(ctx, actor.ID, "staff_reauth_rejected")
	}
	s.limit.forgive("staff_reauth", actor.Username)
	return record(ctx, s.db, actor.ID, "staff_reauth")
}

// BootstrapAdmin can only operate while the singleton bootstrap record is absent.
// An existing account claim requires its password and a saved recovery code.
func (s *Service) BootstrapAdmin(ctx context.Context, password, claimCode string, claim bool) ([]string, error) {
	if !Validate("admin", password) || (claim && !ValidToken(claimCode)) {
		return nil, ErrInvalid
	}
	var codes []string
	err := s.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockRoleAdministration(ctx); err != nil {
			return err
		}
		if _, err := q.BootstrappedAdmin(ctx); err == nil {
			return ErrBootstrapComplete
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		taken, err := q.ActiveAdministratorExists(ctx)
		if err != nil {
			return err
		}
		if taken {
			return ErrBootstrapComplete
		}
		var actor pgtype.UUID
		row, err := q.UserCredential(ctx, "admin")
		switch {
		case err == nil:
			if !claim {
				return ErrAdminNameInUse
			}
			encoded, err := q.LockUser(ctx, row.ID)
			if err != nil {
				return err
			}
			if !passwordMatches(password, encoded) {
				return ErrInvalid
			}
			used, err := q.ConsumeRecoveryCode(ctx, db.ConsumeRecoveryCodeParams{UserID: row.ID, TokenHash: tokenHash(claimCode)})
			if err != nil {
				return err
			}
			if used != 1 {
				return ErrInvalid
			}
			actor = row.ID
			if err := q.DeleteRecoveryCodes(ctx, actor); err != nil {
				return err
			}
			if err := q.DeleteAllSessions(ctx, actor); err != nil {
				return err
			}
		case errors.Is(err, pgx.ErrNoRows):
			if claim {
				return ErrInvalid
			}
			actor, err = q.CreateUser(ctx, db.CreateUserParams{Username: "admin", PasswordHash: passwordHash(password)})
			if err != nil {
				return err
			}
		default:
			return err
		}
		if err := q.RecordBootstrap(ctx, actor); err != nil {
			return err
		}
		if err := setRole(ctx, q, actor, actor, authz.Admin, "bootstrap"); err != nil {
			return err
		}
		codes, err = issueRecoveryCodes(ctx, q, actor)
		return err
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

func issueRecoveryCodes(ctx context.Context, q *db.Queries, id pgtype.UUID) ([]string, error) {
	codes := make([]string, 5)
	for i := range codes {
		codes[i] = NewToken()
		if err := q.StoreRecoveryCode(ctx, db.StoreRecoveryCodeParams{UserID: id, TokenHash: tokenHash(codes[i])}); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// RestoreAdmin only restores the original bootstrapped account, using its current password.
func (s *Service) RestoreAdmin(ctx context.Context, password string) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockRoleAdministration(ctx); err != nil {
			return err
		}
		row, err := q.BootstrappedAdmin(ctx)
		if err != nil {
			return err
		}
		encoded, err := q.LockUser(ctx, row.UserID)
		if err != nil {
			return err
		}
		if !Validate("admin", password) || !passwordMatches(password, encoded) {
			return ErrInvalid
		}
		current, err := q.EffectiveRole(ctx, row.UserID)
		if err != nil || current == authz.Admin {
			return err
		}
		taken, err := q.ActiveAdministratorExists(ctx)
		if err != nil {
			return err
		}
		if taken {
			return ErrBootstrapComplete
		}
		return setRole(ctx, q, row.UserID, row.UserID, authz.Admin, "restore")
	})
}

// OperatorSetRole grants only user or moderator. Elevated access is explicit per account.
func (s *Service) OperatorSetRole(ctx context.Context, password, username, role string) error {
	if (role != authz.User && role != authz.Moderator) || username == "admin" || !ValidUsername(username) {
		return ErrOperator
	}
	return s.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockRoleAdministration(ctx); err != nil {
			return err
		}
		actor, err := q.BootstrappedAdmin(ctx)
		if err != nil {
			return err
		}
		encoded, err := q.LockUser(ctx, actor.UserID)
		if err != nil {
			return err
		}
		current, err := q.EffectiveRole(ctx, actor.UserID)
		if err != nil {
			return err
		}
		if !authz.Allows(current, authz.Platform) || !Validate("admin", password) || !passwordMatches(password, encoded) {
			return ErrOperator
		}
		subject, err := q.UserCredential(ctx, username)
		if err != nil {
			return err
		}
		old, err := q.EffectiveRole(ctx, subject.ID)
		if err != nil {
			return err
		}
		if old == authz.Admin || subject.ID == actor.UserID {
			return ErrOperator
		}
		return setRole(ctx, q, actor.UserID, subject.ID, role, "operator_grant")
	})
}

func setRole(ctx context.Context, q *db.Queries, actor, subject pgtype.UUID, role, action string) error {
	old, err := q.EffectiveRole(ctx, subject)
	if err != nil {
		return err
	}
	if old == role {
		return nil
	}
	if err := q.SetRole(ctx, db.SetRoleParams{UserID: subject, Role: role}); err != nil {
		return err
	}
	if err := q.DeleteAllSessions(ctx, subject); err != nil {
		return err
	}
	return q.RecordRoleChange(ctx, db.RecordRoleChangeParams{ActorID: actor, SubjectID: subject, OldRole: old, NewRole: role, Action: action})
}

// ChangeRole verifies both the operator and the requested previous role under
// the same lock used by bootstrap, local grants and role restoration.
func (s *Service) ChangeRole(ctx context.Context, actor pgtype.UUID, username, previous, next string) error {
	if !ValidUsername(username) || !authz.ValidRole(previous) || !authz.ValidRole(next) {
		return ErrOperator
	}
	return s.inTx(ctx, func(q *db.Queries) error {
		if err := q.LockRoleAdministration(ctx); err != nil {
			return err
		}
		role, err := q.EffectiveRole(ctx, actor)
		if err != nil {
			return err
		}
		if !authz.Allows(role, authz.Platform) {
			return ErrOperator
		}
		subject, err := q.UserCredential(ctx, username)
		if err != nil {
			return err
		}
		current, err := q.EffectiveRole(ctx, subject.ID)
		if err != nil {
			return err
		}
		if current != previous {
			return ErrRoleChanged
		}
		if current == authz.Admin && next != authz.Admin {
			admins, err := q.AdministratorCount(ctx)
			if err != nil {
				return err
			}
			if admins < 2 {
				return ErrLastAdministrator
			}
		}
		return setRole(ctx, q, actor, subject.ID, next, "dashboard_assignment")
	})
}
