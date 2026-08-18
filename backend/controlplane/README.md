# Rock 独立控制面

本目录对应 Rock 新控制面的阶段 0 实现：UUID 用户身份、团队租户、团队成员关系、独立平台角色、显式团队请求范围和只读身份查询 API。

控制面是独立进程和独立数据库。它不启动、调用或读取旧 `cmd/server` 服务，也不要求旧应用、流水线、环境、集群或制品表存在。

## 前置条件

- Go 1.25+
- MariaDB 11+ 或兼容 MySQL
- `migrate` CLI
- 能签发 RS256 JWT 的外部身份提供方

创建专用数据库，不能与旧服务数据库混用：

```sql
CREATE DATABASE rock_control_plane
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;
```

## 配置

API 和 bootstrap 共用以下数据库配置：

```bash
export ROCK_CONTROL_PLANE_DATABASE_DSN='rock_control_plane:password@tcp(127.0.0.1:3306)/rock_control_plane?parseTime=true&loc=UTC'
export ROCK_CONTROL_PLANE_DATABASE_URL='mysql://rock_control_plane:password@tcp(127.0.0.1:3306)/rock_control_plane?multiStatements=true'
```

可选连接池配置：

```bash
export ROCK_CONTROL_PLANE_DATABASE_MAX_OPEN_CONNS=25
export ROCK_CONTROL_PLANE_DATABASE_MAX_IDLE_CONNS=5
export ROCK_CONTROL_PLANE_DATABASE_CONN_MAX_LIFETIME_SECONDS=300
export ROCK_CONTROL_PLANE_DATABASE_CONN_MAX_IDLE_TIME_SECONDS=60
```

API 还要求：

```bash
export ROCK_CONTROL_PLANE_HTTP_ADDRESS=':8090'
export ROCK_CONTROL_PLANE_MODE='release'
export ROCK_CONTROL_PLANE_LOG_LEVEL='info'
export ROCK_CONTROL_PLANE_JWT_ISSUER='https://identity.example.com'
export ROCK_CONTROL_PLANE_JWT_AUDIENCE='rock-control-plane'
export ROCK_CONTROL_PLANE_JWT_PUBLIC_KEY_FILE='/absolute/path/identity-public.pem'
```

公钥文件必须只包含一个 PKIX 或 PKCS#1 RSA 公钥 PEM block，路径必须是绝对路径。控制面只接受 RS256，并要求 token 同时具有有效的 `sub`、`iss`、`aud`、`iat`、`nbf` 和 `exp`。`sub` 必须是已登记 User 的 UUID。

## 迁移

执行独立迁移：

```bash
make controlplane-migrate
```

迁移只创建：

- `users`
- `teams`
- `team_memberships`
- `platform_role_bindings`

迁移目录为 `backend/controlplane/migrations`，不会执行 `backend/migrations` 中的旧迁移。专用数据库中的迁移版本表也与旧服务隔离。

## 一次性初始化

阶段 0 不提供用户、团队或授权写 API。空 schema 必须通过一次性命令建立首个治理主体。

创建 `/secure/path/bootstrap.json`：

```json
{
  "user": {
    "id": "11111111-1111-4111-8111-111111111111",
    "email": "platform-admin@example.com",
    "display_name": "Platform Admin"
  },
  "team": {
    "id": "22222222-2222-4222-8222-222222222222",
    "slug": "platform-team",
    "name": "Platform Team"
  },
  "platform_admin": true,
  "grant_reference": "initial-governance-bootstrap"
}
```

`user.id` 必须与外部身份提供方 JWT 的 `sub` 完全一致。执行：

```bash
cd backend
go run ./cmd/controlplane-bootstrap -manifest /secure/path/bootstrap.json
```

命令仅在四张身份/团队表全部为空时成功，并在一个事务中创建 User、Team、admin Membership 和可选 `platform_admin` Binding。任一表已有记录时会拒绝再次执行。初始化后应删除部署 Job 和 manifest 输入材料；后续变更必须进入 Operation 治理链。

## 启动与验证

运行测试和构建：

```bash
make controlplane-test
make controlplane-build
```

启动 API：

```bash
make controlplane-run
```

健康检查：

```bash
curl http://127.0.0.1:8090/healthz
curl http://127.0.0.1:8090/readyz
```

身份查询：

```bash
export CONTROL_PLANE_TOKEN='<RS256 identity token>'

curl -H "Authorization: Bearer $CONTROL_PLANE_TOKEN" \
  http://127.0.0.1:8090/v1/me

curl -H "Authorization: Bearer $CONTROL_PLANE_TOKEN" \
  http://127.0.0.1:8090/v1/me/teams
```

团队上下文查询必须同时提供路径团队 UUID 和匹配的 active team header：

```bash
export ACTIVE_TEAM_ID='22222222-2222-4222-8222-222222222222'

curl -H "Authorization: Bearer $CONTROL_PLANE_TOKEN" \
  -H "X-Active-Team-Id: $ACTIVE_TEAM_ID" \
  "http://127.0.0.1:8090/v1/teams/$ACTIVE_TEAM_ID/context"
```

JWT 中即使包含团队或角色字段也不会授予权限。团队访问始终由控制面数据库中的当前有效 Membership 决定，平台角色不会隐式产生团队成员关系。
