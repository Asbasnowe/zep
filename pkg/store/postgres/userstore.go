package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/getzep/zep/pkg/models"
	"github.com/uptrace/bun"
)

var _ models.UserStore = &UserStoreDAO{}

type UserStoreDAO struct {
	db *bun.DB
}

func NewUserStoreDAO(db *bun.DB) *UserStoreDAO {
	return &UserStoreDAO{
		db: db,
	}
}

// Create creates a new user.
func (dao *UserStoreDAO) Create(
	ctx context.Context,
	user *models.CreateUserRequest,
) (*models.User, error) {
	userDB := &UserSchema{
		UserID:    user.UserID,
		Email:     user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Metadata:  user.Metadata,
	}
	_, err := dao.db.NewInsert().Model(userDB).Returning("*").Exec(ctx)
	if err != nil {
		return nil, err
	}

	createdUser := &models.User{
		UUID:      userDB.UUID,
		ID:        userDB.ID,
		CreatedAt: userDB.CreatedAt,
		UpdatedAt: userDB.UpdatedAt,
		UserID:    userDB.UserID,
		Email:     userDB.Email,
		FirstName: userDB.FirstName,
		LastName:  userDB.LastName,
		Metadata:  userDB.Metadata,
	}

	return createdUser, err
}

// Get gets a user by UserID.
func (dao *UserStoreDAO) Get(ctx context.Context, userID string) (*models.User, error) {
	user := new(UserSchema)
	err := dao.db.NewSelect().Model(user).Where("user_id = ?", userID).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.NewNotFoundError("user " + userID)
		}
		return nil, err
	}
	return userSchemaToUser(user), nil
}

// Update updates a user.
func (dao *UserStoreDAO) Update(
	ctx context.Context,
	user *models.UpdateUserRequest,
	isPrivileged bool,
) (*models.User, error) {
	if user.UserID == "" {
		return nil, errors.New("UserID cannot be empty")
	}

	// if metadata is null, we can keep this a cheap operation
	if user.Metadata == nil {
		return dao.updateUser(ctx, user)
	}

	// Acquire a lock for this UserID. This is to prevent concurrent updates
	// to the session metadata.
	lockID, err := acquireAdvisoryLock(ctx, dao.db, user.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}
	defer func(ctx context.Context, db bun.IDB, lockID uint64) {
		err := releaseAdvisoryLock(ctx, db, lockID)
		if err != nil {
			log.Errorf("failed to release advisory lock: %v", err)
		}
	}(ctx, dao.db, lockID)

	mergedMetadata, err := mergeMetadata(
		ctx,
		dao.db,
		"user_id",
		user.UserID,
		"users",
		user.Metadata,
		isPrivileged,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to merge metadata: %w", err)
	}

	user.Metadata = mergedMetadata
	return dao.updateUser(ctx, user)
}

func (dao *UserStoreDAO) updateUser(
	ctx context.Context,
	user *models.UpdateUserRequest,
) (*models.User, error) {
	userDB := UserSchema{
		Email:     user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Metadata:  user.Metadata,
	}
	r, err := dao.db.NewUpdate().
		Model(&userDB).
		Column("email", "first_name", "last_name", "metadata").
		OmitZero().
		Where("user_id = ?", user.UserID).
		// We can't use Returning("*") here asc it breaks OmitZero()
		//Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := r.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, models.NewNotFoundError("user " + user.UserID)
	}

	// We're can't return the updated User above as we're using OmitZero,
	// so we need to get the updated user from the DB
	updatedUserDB, err := dao.Get(ctx, user.UserID)
	if err != nil {
		return nil, err
	}

	updatedUser := &models.User{
		UUID:      updatedUserDB.UUID,
		ID:        updatedUserDB.ID,
		CreatedAt: updatedUserDB.CreatedAt,
		UpdatedAt: updatedUserDB.UpdatedAt,
		UserID:    updatedUserDB.UserID,
		Email:     updatedUserDB.Email,
		FirstName: updatedUserDB.FirstName,
		LastName:  updatedUserDB.LastName,
		Metadata:  updatedUserDB.Metadata,
	}

	return updatedUser, nil
}

// Delete deletes a user.
func (dao *UserStoreDAO) Delete(ctx context.Context, userID string) error {
	r, err := dao.db.NewDelete().Model(&models.User{}).Where("user_id = ?", userID).Exec(ctx)
	if err != nil {
		return err
	}
	rowsAffected, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return models.NewNotFoundError("user " + userID)
	}

	return nil
}

// ListAll lists all users. The cursor is used to paginate results.
func (dao *UserStoreDAO) ListAll(
	ctx context.Context,
	cursor int64,
	limit int,
) ([]*models.User, error) {
	var usersDB []*UserSchema
	err := dao.db.NewSelect().
		Model(&usersDB).
		Where("id > ?", cursor).
		OrderExpr("id ASC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	users := make([]*models.User, len(usersDB))
	for i := range users {
		users[i] = userSchemaToUser(usersDB[i])
	}

	return users, nil
}

// GetSessions gets all sessions for a user.
func (dao *UserStoreDAO) GetSessions(
	ctx context.Context,
	userID string,
) ([]*models.Session, error) {
	var sessionsDB []*SessionSchema
	err := dao.db.NewSelect().
		Model(&sessionsDB).
		Join("JOIN users u ON u.user_id = s.user_id").
		Where("u.user_id = ?", userID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	sessions := make([]*models.Session, len(sessionsDB))
	for i := range sessions {
		sessions[i] = &models.Session{
			UUID:      sessionsDB[i].UUID,
			CreatedAt: sessionsDB[i].CreatedAt,
			UpdatedAt: sessionsDB[i].UpdatedAt,
			SessionID: sessionsDB[i].SessionID,
			Metadata:  sessionsDB[i].Metadata,
			UserID:    sessionsDB[i].UserID,
		}
	}
	return sessions, nil
}

// CountAll counts all users.
func (dao *UserStoreDAO) CountAll(ctx context.Context) (int, error) {
	count, err := dao.db.NewSelect().Model(&UserSchema{}).Count(ctx)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func userSchemaToUser(user *UserSchema) *models.User {
	return &models.User{
		UUID:      user.UUID,
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		UserID:    user.UserID,
		Email:     user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Metadata:  user.Metadata,
	}
}
