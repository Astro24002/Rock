package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"rock/internal/controlplane/bootstrap"
	"rock/internal/controlplane/identity"
	"rock/internal/controlplane/team"
)

type Repository struct {
	db *gorm.DB
}

const bootstrapLockName = "rock_control_plane_bootstrap"

// The process lock covers SQLite and the named MySQL lock covers separate bootstrap processes.
var bootstrapProcessLock sync.Mutex

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func AutoMigrateForTest(db *gorm.DB) error {
	if err := db.AutoMigrate(&userRow{}, &teamRow{}, &membershipRow{}, &platformRoleBindingRow{}); err != nil {
		return err
	}
	return db.Exec("CREATE UNIQUE INDEX uk_memberships_one_active ON team_memberships(user_id, team_id) WHERE status = 'active'").Error
}

func (r *Repository) Ping(ctx context.Context) error {
	sqlDB, err := r.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func (r *Repository) ApplyBootstrap(ctx context.Context, seed bootstrap.Seed) (bootstrap.Result, error) {
	var result bootstrap.Result
	bootstrapProcessLock.Lock()
	defer bootstrapProcessLock.Unlock()

	apply := func(db *gorm.DB) error {
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			empty, err := identitySchemaEmpty(tx)
			if err != nil {
				return err
			}
			if !empty {
				return bootstrap.ErrSchemaNotEmpty
			}

			user := rowFromUser(seed.User)
			if err := tx.Create(&user).Error; err != nil {
				return fmt.Errorf("create bootstrap user: %w", err)
			}
			teamRecord := rowFromTeam(seed.Team)
			if err := tx.Create(&teamRecord).Error; err != nil {
				return fmt.Errorf("create bootstrap team: %w", err)
			}
			membership := rowFromMembership(seed.Membership)
			if err := tx.Omit("Team", "User").Create(&membership).Error; err != nil {
				return fmt.Errorf("create bootstrap membership: %w", err)
			}

			result = bootstrap.Result{UserID: seed.User.ID, TeamID: seed.Team.ID, MembershipID: seed.Membership.ID}
			if seed.PlatformRoleBinding != nil {
				binding := rowFromPlatformRole(*seed.PlatformRoleBinding)
				if err := tx.Omit("User").Create(&binding).Error; err != nil {
					return fmt.Errorf("create bootstrap platform role: %w", err)
				}
				id := seed.PlatformRoleBinding.ID
				result.PlatformRoleBindingID = &id
			}
			return nil
		})
	}

	db := r.db.WithContext(ctx)
	var err error
	if db.Dialector.Name() == "mysql" {
		err = db.Connection(func(conn *gorm.DB) (err error) {
			var acquired sql.NullInt64
			row := conn.Raw("SELECT GET_LOCK(?, ?)", bootstrapLockName, 10).Row()
			if err := row.Scan(&acquired); err != nil {
				return fmt.Errorf("acquire bootstrap lock: %w", err)
			}
			if !acquired.Valid || acquired.Int64 != 1 {
				return errors.New("control plane bootstrap lock is unavailable")
			}
			defer func() {
				if releaseErr := conn.Exec("SELECT RELEASE_LOCK(?)", bootstrapLockName).Error; err == nil && releaseErr != nil {
					err = fmt.Errorf("release bootstrap lock: %w", releaseErr)
				}
			}()
			return apply(conn)
		})
	} else {
		err = apply(db)
	}
	if err != nil {
		return bootstrap.Result{}, err
	}
	return result, nil
}

func identitySchemaEmpty(db *gorm.DB) (bool, error) {
	for _, table := range []string{"users", "teams", "team_memberships", "platform_role_bindings"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			return false, fmt.Errorf("count %s: %w", table, err)
		}
		if count != 0 {
			return false, nil
		}
	}
	return true, nil
}

func (r *Repository) FindUser(ctx context.Context, id uuid.UUID) (identity.User, error) {
	var row userRow
	err := r.db.WithContext(ctx).Where("id = ?", id.String()).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return identity.User{}, identity.ErrUserNotFound
	}
	if err != nil {
		return identity.User{}, fmt.Errorf("find user: %w", err)
	}
	return mapUser(row)
}

func (r *Repository) ListEffectiveMemberships(ctx context.Context, userID uuid.UUID, now time.Time) ([]team.MembershipContext, error) {
	var rows []membershipRow
	err := effectiveMembershipQuery(r.db.WithContext(ctx), userID, now).
		Order("teams.slug ASC").
		Preload("Team").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list effective memberships: %w", err)
	}

	result := make([]team.MembershipContext, 0, len(rows))
	for i := range rows {
		mapped, err := mapMembershipContext(rows[i])
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func (r *Repository) FindEffectiveMembership(ctx context.Context, userID, teamID uuid.UUID, now time.Time) (team.MembershipContext, error) {
	var row membershipRow
	err := effectiveMembershipQuery(r.db.WithContext(ctx), userID, now).
		Where("team_memberships.team_id = ?", teamID.String()).
		Preload("Team").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return team.MembershipContext{}, team.ErrAccessDenied
	}
	if err != nil {
		return team.MembershipContext{}, fmt.Errorf("find effective membership: %w", err)
	}
	return mapMembershipContext(row)
}

func effectiveMembershipQuery(db *gorm.DB, userID uuid.UUID, now time.Time) *gorm.DB {
	return db.Model(&membershipRow{}).
		Select("team_memberships.*").
		Joins("JOIN users ON users.id = team_memberships.user_id").
		Joins("JOIN teams ON teams.id = team_memberships.team_id").
		Where("team_memberships.user_id = ?", userID.String()).
		Where("users.status = ?", string(identity.UserStatusActive)).
		Where("teams.status = ?", string(team.TeamStatusActive)).
		Where("team_memberships.status = ?", string(team.MembershipStatusActive)).
		Where("team_memberships.effective_at <= ?", now.UTC()).
		Where("team_memberships.expires_at IS NULL OR team_memberships.expires_at > ?", now.UTC())
}

func mapUser(row userRow) (identity.User, error) {
	id, err := uuid.Parse(row.ID)
	if err != nil {
		return identity.User{}, fmt.Errorf("invalid stored user id: %w", err)
	}
	return identity.User{
		ID: id, Email: normalizeEmail(row.Email), DisplayName: row.DisplayName,
		Status: identity.UserStatus(row.Status), ProfileVersion: row.ProfileVersion,
		CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}, nil
}

func mapMembershipContext(row membershipRow) (team.MembershipContext, error) {
	membershipID, err := uuid.Parse(row.ID)
	if err != nil {
		return team.MembershipContext{}, fmt.Errorf("invalid stored membership id: %w", err)
	}
	teamID, err := uuid.Parse(row.TeamID)
	if err != nil {
		return team.MembershipContext{}, fmt.Errorf("invalid stored team id: %w", err)
	}
	userID, err := uuid.Parse(row.UserID)
	if err != nil {
		return team.MembershipContext{}, fmt.Errorf("invalid stored membership user id: %w", err)
	}
	return team.MembershipContext{
		Team: team.Team{
			ID: teamID, Slug: row.Team.Slug, Name: row.Team.Name, Status: team.TeamStatus(row.Team.Status),
			ConfigVersion: row.Team.ConfigVersion, CreatedAt: row.Team.CreatedAt.UTC(), UpdatedAt: row.Team.UpdatedAt.UTC(),
		},
		Membership: team.Membership{
			ID: membershipID, TeamID: teamID, UserID: userID, Role: team.Role(row.Role), Status: team.MembershipStatus(row.Status),
			EffectiveAt: row.EffectiveAt.UTC(), ExpiresAt: utcPointer(row.ExpiresAt), Source: row.Source, Version: row.Version,
			CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
		},
	}, nil
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func normalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func rowFromUser(value identity.User) userRow {
	return userRow{
		ID: value.ID.String(), Email: normalizeEmail(value.Email), DisplayName: value.DisplayName,
		Status: string(value.Status), ProfileVersion: value.ProfileVersion,
		CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(),
	}
}

func rowFromTeam(value team.Team) teamRow {
	return teamRow{
		ID: value.ID.String(), Slug: value.Slug, Name: value.Name, Status: string(value.Status),
		ConfigVersion: value.ConfigVersion, CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(),
	}
}

func rowFromMembership(value team.Membership) membershipRow {
	return membershipRow{
		ID: value.ID.String(), TeamID: value.TeamID.String(), UserID: value.UserID.String(), Role: string(value.Role),
		Status: string(value.Status), EffectiveAt: value.EffectiveAt.UTC(), ExpiresAt: utcPointer(value.ExpiresAt),
		Source: value.Source, Version: value.Version, CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(),
	}
}

func rowFromPlatformRole(value team.PlatformRoleBinding) platformRoleBindingRow {
	return platformRoleBindingRow{
		ID: value.ID.String(), UserID: value.UserID.String(), Role: string(value.Role), Scope: string(value.Scope),
		EffectiveAt: value.EffectiveAt.UTC(), ExpiresAt: utcPointer(value.ExpiresAt), GrantReference: value.GrantReference,
		Status: string(value.Status), Version: value.Version, CreatedAt: value.CreatedAt.UTC(), UpdatedAt: value.UpdatedAt.UTC(),
	}
}
