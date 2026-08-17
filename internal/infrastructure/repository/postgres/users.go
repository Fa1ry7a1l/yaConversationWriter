package postgres

import (
	"context"
	"fmt"
	"strings"

	"yaConversationWriter/internal/domain"
)

func (r *Repository) GetOrCreateUser(ctx context.Context, externalID string) (domain.User, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return domain.User{}, fmt.Errorf("%w: external user ID is required", domain.ErrInvalidArgument)
	}
	row := r.pool.QueryRow(ctx, `
        INSERT INTO users (external_id)
        VALUES ($1)
        ON CONFLICT (external_id) DO UPDATE SET external_id = EXCLUDED.external_id
        RETURNING id::text, external_id, created_at, updated_at
    `, externalID)
	user, err := scanUser(row)
	if err != nil {
		return domain.User{}, r.mapError("get or create user", err)
	}
	return user, nil
}

func (r *Repository) GetUserByExternalID(ctx context.Context, externalID string) (domain.User, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return domain.User{}, fmt.Errorf("%w: external user ID is required", domain.ErrInvalidArgument)
	}
	user, err := scanUser(r.pool.QueryRow(ctx, `
        SELECT id::text, external_id, created_at, updated_at
        FROM users
        WHERE external_id = $1
    `, externalID))
	if err != nil {
		return domain.User{}, r.mapError("get user by external ID", err)
	}
	return user, nil
}

func scanUser(row scanner) (domain.User, error) {
	var user domain.User
	var id string
	if err := row.Scan(&id, &user.ExternalID, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return domain.User{}, err
	}
	user.ID = domain.UserID(id)
	return user, nil
}
