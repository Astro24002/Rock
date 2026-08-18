CREATE TABLE users (
    id CHAR(36) PRIMARY KEY,
    email VARCHAR(320) NOT NULL,
    display_name VARCHAR(200) NOT NULL,
    status VARCHAR(20) NOT NULL,
    profile_version INT NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT chk_users_status CHECK (status IN ('active', 'suspended', 'disabled')),
    CONSTRAINT chk_users_profile_version CHECK (profile_version > 0),
    UNIQUE KEY uk_users_email (email),
    KEY idx_users_status (status)
);

CREATE TABLE teams (
    id CHAR(36) PRIMARY KEY,
    slug VARCHAR(63) NOT NULL,
    name VARCHAR(200) NOT NULL,
    status VARCHAR(20) NOT NULL,
    config_version INT NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT chk_teams_status CHECK (status IN ('active', 'suspended', 'disabled')),
    CONSTRAINT chk_teams_config_version CHECK (config_version > 0),
    UNIQUE KEY uk_teams_slug (slug),
    KEY idx_teams_status (status)
);

CREATE TABLE team_memberships (
    id CHAR(36) PRIMARY KEY,
    team_id CHAR(36) NOT NULL,
    user_id CHAR(36) NOT NULL,
    role VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL,
    effective_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6),
    source VARCHAR(100) NOT NULL,
    version INT NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    active_membership_marker TINYINT
        GENERATED ALWAYS AS (IF(status = 'active', 1, NULL)) STORED,
    CONSTRAINT fk_memberships_team FOREIGN KEY (team_id) REFERENCES teams(id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_memberships_user FOREIGN KEY (user_id) REFERENCES users(id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT chk_memberships_role CHECK (role IN ('viewer', 'developer', 'admin')),
    CONSTRAINT chk_memberships_status CHECK (status IN ('active', 'revoked', 'expired')),
    CONSTRAINT chk_memberships_expiry CHECK (expires_at IS NULL OR expires_at > effective_at),
    CONSTRAINT chk_memberships_version CHECK (version > 0),
    UNIQUE KEY uk_memberships_one_active (user_id, team_id, active_membership_marker),
    KEY idx_memberships_user_status (user_id, status, effective_at, expires_at),
    KEY idx_memberships_team_status (team_id, status)
);

CREATE TABLE platform_role_bindings (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    role VARCHAR(30) NOT NULL,
    scope VARCHAR(20) NOT NULL,
    effective_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6),
    grant_reference VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL,
    version INT NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT fk_platform_roles_user FOREIGN KEY (user_id) REFERENCES users(id) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT chk_platform_roles_role CHECK (role IN ('platform_admin', 'asset_operator', 'auditor')),
    CONSTRAINT chk_platform_roles_scope CHECK (scope = 'platform'),
    CONSTRAINT chk_platform_roles_expiry CHECK (expires_at IS NULL OR expires_at > effective_at),
    CONSTRAINT chk_platform_roles_status CHECK (status IN ('active', 'revoked', 'expired')),
    CONSTRAINT chk_platform_roles_version CHECK (version > 0),
    KEY idx_platform_roles_user_status (user_id, status)
);
