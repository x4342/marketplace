package administration

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/x4342/marketplace/internal/authz"
	"github.com/x4342/marketplace/internal/db"
)

type Service struct{ Pool *pgxpool.Pool }

type Category struct {
	ID           pgtype.UUID
	Revision     int64
	Name         string
	Slug         string
	Description  string
	Parent       pgtype.UUID
	DisplayOrder int32
	Active       bool
}

var (
	ErrForbidden = errors.New("administrator access required")
	ErrChanged   = errors.New("This category changed. Reopen it and try again.")
	ErrName      = errors.New("Enter a category name of 1 to 100 characters and a description of up to 1,000 characters.")
	ErrSlug      = errors.New("Use a slug of at most 80 lowercase letters, numbers, and single hyphens.")
	ErrTaken     = errors.New("That slug is already in use or was previously used by another category.")
	ErrParent    = errors.New("Choose an existing top-level category. Categories support two levels and cannot contain themselves.")
	ErrOrder     = errors.New("Enter a display position between 0 and 9999.")
	slugPattern  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	slugBreak    = regexp.MustCompile(`[^a-z0-9]+`)
)

func (s Service) SaveCategory(ctx context.Context, actor pgtype.UUID, c Category) error {
	c.Name = strings.TrimSpace(c.Name)
	c.Description = strings.TrimSpace(c.Description)
	if c.Slug == "" {
		c.Slug = strings.Trim(slugBreak.ReplaceAllString(strings.ToLower(c.Name), "-"), "-")
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		if err := q.LockRoleAdministration(ctx); err != nil {
			return err
		}
		role, err := q.EffectiveRole(ctx, actor)
		if err != nil {
			return err
		}
		if !authz.Allows(role, authz.Platform) {
			return ErrForbidden
		}
		if !validText(c.Name, 100) || c.Name == "" || !validText(c.Description, 1000) {
			return ErrName
		}
		if len(c.Slug) > 80 || !slugPattern.MatchString(c.Slug) {
			return ErrSlug
		}
		if c.DisplayOrder < 0 || c.DisplayOrder > 9999 {
			return ErrOrder
		}
		if err := q.LockCategories(ctx); err != nil {
			return err
		}
		var parent pgtype.Text
		if c.Parent.Valid {
			p, err := q.AdminCategory(ctx, db.AdminCategoryParams{ID: c.Parent, Actor: actor})
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrParent
			}
			if err != nil {
				return err
			}
			if p.ParentSlug.Valid || p.ID == c.ID {
				return ErrParent
			}
			parent = pgtype.Text{String: p.Slug, Valid: true}
		}
		var before []byte
		if c.ID.Valid {
			before, err = q.CategoryAuditState(ctx, db.CategoryAuditStateParams{ID: c.ID, Actor: actor})
			if err != nil {
				return err
			}
			count, err := q.UpdateCategory(ctx, db.UpdateCategoryParams{ID: c.ID, Revision: c.Revision, Actor: actor, Name: c.Name, Slug: c.Slug, Description: c.Description, ParentSlug: parent, DisplayOrder: c.DisplayOrder, Active: c.Active})
			if err != nil {
				return err
			}
			if count != 1 {
				return ErrChanged
			}
		} else {
			c.ID, err = q.CreateCategory(ctx, db.CreateCategoryParams{Actor: actor, Name: c.Name, Slug: c.Slug, Description: c.Description, ParentSlug: parent, DisplayOrder: c.DisplayOrder, Active: c.Active})
			if err != nil {
				return err
			}
		}
		return q.RecordCategoryChange(ctx, db.RecordCategoryChangeParams{ID: c.ID, Actor: actor, BeforeState: before})
	})
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) {
		switch postgres.Code {
		case "23505":
			return ErrTaken
		case "23514", "23503":
			return ErrParent
		}
	}
	return err
}

func validText(value string, limit int) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= limit
}
