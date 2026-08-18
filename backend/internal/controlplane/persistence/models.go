package persistence

import "time"

type userRow struct {
	ID             string    `gorm:"type:char(36);primaryKey"`
	Email          string    `gorm:"type:varchar(320);not null;uniqueIndex:uk_users_email"`
	DisplayName    string    `gorm:"type:varchar(200);not null"`
	Status         string    `gorm:"type:varchar(20);not null;index;check:chk_users_status,status IN ('active','suspended','disabled')"`
	ProfileVersion int       `gorm:"not null;default:1;check:chk_users_profile_version,profile_version > 0"`
	CreatedAt      time.Time `gorm:"not null"`
	UpdatedAt      time.Time `gorm:"not null"`
}

func (userRow) TableName() string { return "users" }

type teamRow struct {
	ID            string    `gorm:"type:char(36);primaryKey"`
	Slug          string    `gorm:"type:varchar(63);not null;uniqueIndex:uk_teams_slug"`
	Name          string    `gorm:"type:varchar(200);not null"`
	Status        string    `gorm:"type:varchar(20);not null;index;check:chk_teams_status,status IN ('active','suspended','disabled')"`
	ConfigVersion int       `gorm:"not null;default:1;check:chk_teams_config_version,config_version > 0"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
}

func (teamRow) TableName() string { return "teams" }

type membershipRow struct {
	ID          string     `gorm:"type:char(36);primaryKey"`
	TeamID      string     `gorm:"type:char(36);not null;index:idx_memberships_team_status,priority:1"`
	UserID      string     `gorm:"type:char(36);not null;index:idx_memberships_user_status,priority:1"`
	Role        string     `gorm:"type:varchar(20);not null;check:chk_memberships_role,role IN ('viewer','developer','admin')"`
	Status      string     `gorm:"type:varchar(20);not null;index:idx_memberships_team_status,priority:2;index:idx_memberships_user_status,priority:2;check:chk_memberships_status,status IN ('active','revoked','expired')"`
	EffectiveAt time.Time  `gorm:"not null;index:idx_memberships_user_status,priority:3"`
	ExpiresAt   *time.Time `gorm:"index:idx_memberships_user_status,priority:4;check:chk_memberships_expiry,expires_at IS NULL OR expires_at > effective_at"`
	Source      string     `gorm:"type:varchar(100);not null"`
	Version     int        `gorm:"not null;default:1;check:chk_memberships_version,version > 0"`
	CreatedAt   time.Time  `gorm:"not null"`
	UpdatedAt   time.Time  `gorm:"not null"`
	Team        teamRow    `gorm:"foreignKey:TeamID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	User        userRow    `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (membershipRow) TableName() string { return "team_memberships" }

type platformRoleBindingRow struct {
	ID             string     `gorm:"type:char(36);primaryKey"`
	UserID         string     `gorm:"type:char(36);not null;index:idx_platform_roles_user_status,priority:1"`
	Role           string     `gorm:"type:varchar(30);not null;check:chk_platform_roles_role,role IN ('platform_admin','asset_operator','auditor')"`
	Scope          string     `gorm:"type:varchar(20);not null;check:chk_platform_roles_scope,scope = 'platform'"`
	EffectiveAt    time.Time  `gorm:"not null"`
	ExpiresAt      *time.Time `gorm:"check:chk_platform_roles_expiry,expires_at IS NULL OR expires_at > effective_at"`
	GrantReference string     `gorm:"type:varchar(255);not null"`
	Status         string     `gorm:"type:varchar(20);not null;index:idx_platform_roles_user_status,priority:2;check:chk_platform_roles_status,status IN ('active','revoked','expired')"`
	Version        int        `gorm:"not null;default:1;check:chk_platform_roles_version,version > 0"`
	CreatedAt      time.Time  `gorm:"not null"`
	UpdatedAt      time.Time  `gorm:"not null"`
	User           userRow    `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (platformRoleBindingRow) TableName() string { return "platform_role_bindings" }
