# Rock 控制面阶段 0：身份、团队与请求范围设计

## 状态

本文定义新 Rock 控制面的第一条工程纵切。实现完全以当前控制面架构文档为基线，不兼容、不迁移也不依赖旧 Phase 1 服务、旧 API、旧认证、旧业务模型或旧数据库表。

本文覆盖身份主体、团队租户、团队成员关系、平台角色绑定、认证边界和不可变 `RequestScope`。Operation、Outbox、审批、执行器、Service、Environment、TeamAsset 和外部系统集成不在本阶段实现。

## 目标

阶段 0 交付一个可独立启动、可在空数据库上运行的新控制面单体，并通过自动化测试证明以下安全不变量：

1. 用户主体使用不可变 UUID；邮箱不参与授权主键。
2. 用户可以属于多个团队，但每次团队请求只有一个显式 `active_team_id`。
3. 每次团队请求都重新校验当前有效成员关系，不把 token 中的团队或角色声明作为最终授权依据。
4. 团队角色与平台角色相互独立，不存在隐式继承。
5. 缺失、非法、过期或跨团队的请求范围在进入业务 handler 前被拒绝。
6. 新控制面启动和测试不需要任何旧服务、旧表或旧配置。

## 非目标

本阶段不包含：

- 兼容旧管理员用户名/密码登录或旧 JWT claim。
- 迁移旧应用、流水线、环境、集群、制品或审计数据。
- 给旧业务表补充 `team_id`。
- 创建、更新或删除 Team、Membership、PlatformRoleBinding 的公开管理 API。
- Operation、RiskAssessment、Approval、Execution、Outbox、事件流或审计原始存储。
- Service、Environment、TeamAsset、PlatformAsset 或 ServiceCatalog。
- 浏览器团队选择界面。
- 自定义团队角色、关系图授权或跨团队共享。

这些能力必须在后续独立规格中按最新架构继续设计，不能通过调用旧服务临时补齐。

## 方案决策

新控制面采用独立模块化单体，而不是改造旧服务或立即拆分微服务。

```text
cmd/controlplane
  -> controlplane transport/http
  -> controlplane application services
  -> controlplane identity/team domain
  -> controlplane repositories
  -> controlplane database
```

选择模块化单体的原因：

- 当前阶段需要本地事务、明确边界和快速验证，不需要分布式一致性。
- 后续 Operation、Outbox 和投影可以沿领域接口加入同一控制面进程。
- 包边界和数据库所有权先稳定，达到架构定义的拆分触发条件后再独立部署。

新代码不得导入旧 `internal/service`、`internal/repository`、`internal/middleware`、`internal/model` 或 `cmd/server`。允许复用 Go 标准库和仓库已经选定的通用第三方库，但不能复用带旧业务语义的包。

## 运行与配置边界

新增独立入口 `backend/cmd/controlplane`。它拥有自己的配置前缀、数据库初始化、HTTP 路由和健康检查。

配置前缀固定为 `ROCK_CONTROL_PLANE_`，至少包含：

- HTTP 地址。
- 数据库 DSN 和连接池参数。
- JWT issuer、audience 和签名验证材料。
- 服务运行模式和日志级别。

启动时必须校验认证和数据库必需配置。缺少签名验证材料时服务拒绝启动，不回退到匿名、固定管理员或开发后门。

新迁移放在 `backend/controlplane/migrations`，使用独立迁移版本表。控制面可在只有新 schema 的空数据库中启动，不执行或检查 `backend/migrations` 下的旧迁移。

## 身份认证边界

HTTP 层通过 `Authenticator` 接口验证 Bearer token：

```go
type Principal struct {
    Subject       uuid.UUID
    Email         string
    AuthenticatedAt time.Time
}

type Authenticator interface {
    Authenticate(ctx context.Context, bearerToken string) (Principal, error)
}
```

生产入口装配配置化 JWT 验证器，并至少验证：

- 签名算法在显式白名单中。
- 签名有效。
- `iss` 与配置完全匹配。
- `aud` 包含控制面 audience。
- `sub` 是规范 UUID。
- `exp`、`nbf` 和 `iat` 满足时间约束。

JWT 只证明身份。即使 token 含有团队或角色 claim，控制面也忽略这些 claim，并从权威数据库读取当前 Membership 或 PlatformRoleBinding。

认证成功后，`UserResolver` 按 `sub` 查询用户。首阶段不自动创建未知用户：未登记主体返回 `IDENTITY_NOT_REGISTERED`。这避免任意合法 IdP 用户自动获得控制面主体和团队可见性。用户供给将在后续受控平台 Operation 中实现；部署初始化使用下述独立管理命令，不暴露公共 bootstrap API。

### 一次性初始化

阶段 0 在尚未具备 Operation 写链路时提供独立的 `backend/cmd/controlplane-bootstrap` 管理命令，用于创建首个 User、Team、admin Membership 和可选的 `platform_admin` Binding。它不是 HTTP API，也不包含默认账号或默认密码。

初始化命令必须满足：

- 只接受本地 JSON manifest，显式提供与 IdP `sub` 一致的 User UUID、规范化 email、Team UUID、slug、名称和授予依据。
- 启动前确认四张身份/团队表均为空；任一表已有记录就拒绝执行。
- 在单个数据库事务中校验并写入全部记录，任一失败整体回滚。
- 输出创建对象的 UUID 和不含敏感信息的摘要，不输出 token 或签名材料。
- 不支持更新、追加成员或重复执行。后续身份与成员变更必须进入 Operation 治理链。

该命令是绿地系统建立首个治理主体的唯一阶段 0 例外。部署系统应将 manifest 作为受控一次性输入，并在初始化后移除该 Job 和输入材料。

## 领域模型

### User

`users` 保存全局身份主体：

| 字段 | 约束 |
| --- | --- |
| `id` | UUID 主键，不可变。与身份提供方 `sub` 对齐。 |
| `email` | 规范化小写，全局唯一；可变，不用于授权。 |
| `display_name` | 展示资料，不参与权限判断。 |
| `status` | `active`、`suspended`、`disabled`。只有 `active` 可建立请求范围。 |
| `profile_version` | 正整数，更新资料时递增。 |
| `created_at`, `updated_at` | UTC 时间。 |

### Team

`teams` 保存业务租户边界：

| 字段 | 约束 |
| --- | --- |
| `id` | UUID 主键，即不可变 `team_id`。 |
| `slug` | 规范化稳定标识，全局唯一，创建后不可复用。 |
| `name` | 展示名称。 |
| `status` | `active`、`suspended`、`disabled`。 |
| `config_version` | 正整数，配置变化时递增。 |
| `created_at`, `updated_at` | UTC 时间。 |

### TeamMembership

`team_memberships` 保存用户在团队中的权威资格：

| 字段 | 约束 |
| --- | --- |
| `id` | UUID 主键。 |
| `team_id`, `user_id` | 非空外键。 |
| `role` | 固定为 `viewer`、`developer` 或 `admin`。 |
| `status` | `active`、`revoked`、`expired`。 |
| `effective_at` | 生效时间，非空。 |
| `expires_at` | 可空；存在时必须晚于 `effective_at`。 |
| `source` | 受控来源标识。 |
| `version` | 乐观并发版本，初始为 1。 |
| `created_at`, `updated_at` | UTC 时间。 |

同一 `user_id + team_id` 最多存在一条 `active` 关系。仓储写入在事务内强制该不变量，数据库索引支持按用户、团队和状态查询。历史关系不覆盖或删除。

成员关系在以下条件全部满足时有效：用户 active、团队 active、membership active、`effective_at <= now`，并且 `expires_at` 为空或 `now < expires_at`。

### PlatformRoleBinding

`platform_role_bindings` 保存独立的平台资格：

| 字段 | 约束 |
| --- | --- |
| `id` | UUID 主键。 |
| `user_id` | 非空外键。 |
| `role` | `platform_admin`、`asset_operator` 或 `auditor`。 |
| `scope` | 首版固定为 `platform`。 |
| `effective_at`, `expires_at` | 生效区间。 |
| `grant_reference` | 受控授予依据，不可为空。 |
| `status` | `active`、`revoked`、`expired`。 |
| `version` | 乐观并发版本。 |
| `created_at`, `updated_at` | UTC 时间。 |

该表没有 `team_id`。平台角色查询不能返回或合成 TeamMembership。

## RequestScope

认证和范围中间件构造不可变请求值：

```go
type RequestScope struct {
    RequestID       string
    TraceID         string
    ActorUserID     uuid.UUID
    ScopeType       ScopeType
    ActiveTeamID    *uuid.UUID
    MembershipID    *uuid.UUID
    MembershipRole  *TeamRole
    AuthenticatedAt time.Time
}
```

团队请求构造顺序：

```text
verify bearer token
  -> resolve active User by UUID
  -> parse exactly one X-Active-Team-Id
  -> load active Team
  -> load currently effective TeamMembership
  -> construct RequestScope
  -> call team-scoped handler
```

约束如下：

- 团队 API 必须提供单个 `X-Active-Team-Id: <UUID>`。
- 若路径含 `:team_id`，它必须与 header 完全相等。
- header 不得包含逗号列表、多个值或非规范 UUID。
- 团队 scope 的 `ActiveTeamID`、`MembershipID` 和 `MembershipRole` 必须同时存在。
- 平台 scope 的三个团队字段必须全部为空，并由单独的平台授权中间件建立。
- handler 和 application service 只从 `RequestScope` 获取主体及范围，不重新解析 header，也不接受请求体中的 `actor_user_id` 或 `team_id` 覆盖它。
- repository 方法必须显式接收 scope 或 `team_id`；后续团队实体不得提供无范围的通用 `Get(id)` 和 `List()`。

## HTTP API

第一阶段公开以下接口：

```text
GET /healthz
GET /readyz
GET /v1/me
GET /v1/me/teams
GET /v1/teams/:team_id/context
```

### `GET /healthz`

只表示进程存活，不访问数据库，也不要求认证。

### `GET /readyz`

验证数据库连接和必需运行配置已加载，不返回敏感配置。

### `GET /v1/me`

要求认证，不要求团队 header。返回当前 User 的 UUID、email、display name 和 status。

### `GET /v1/me/teams`

要求认证，不要求团队 header。只返回当前时刻有效的 TeamMembership 及对应 active Team，按 team slug 稳定排序。失效、撤销、过期或 suspended team 不返回。

### `GET /v1/teams/:team_id/context`

要求认证和 `X-Active-Team-Id`。路径 UUID 与 header 必须相等。返回当前 Team 的公开上下文字段、Membership UUID 和固定团队角色，用于证明 RequestScope 已建立。

所有成功响应使用新控制面统一 envelope：

```json
{
  "data": {},
  "request_id": "..."
}
```

所有错误响应使用：

```json
{
  "error": {
    "code": "TEAM_CONTEXT_REQUIRED",
    "message": "active team context is required"
  },
  "request_id": "..."
}
```

首阶段稳定错误码：

| HTTP | code | 含义 |
| --- | --- | --- |
| 401 | `AUTHENTICATION_REQUIRED` | 缺少或格式错误的 Bearer token。 |
| 401 | `INVALID_IDENTITY_TOKEN` | token 签名、issuer、audience、subject 或时效无效。 |
| 403 | `IDENTITY_NOT_REGISTERED` | token 有效但不存在已登记用户。 |
| 403 | `IDENTITY_INACTIVE` | 用户不是 active。 |
| 400 | `TEAM_CONTEXT_REQUIRED` | 团队 API 缺少 active team header。 |
| 400 | `INVALID_TEAM_CONTEXT` | header 非单一规范 UUID，或路径与 header 不一致。 |
| 403 | `TEAM_ACCESS_DENIED` | 团队、成员关系或有效期不满足；响应不区分团队不存在与无权限。 |
| 500 | `INTERNAL_ERROR` | 未映射内部错误；响应不暴露数据库或 token 细节。 |

## 数据访问与事务

- repository 接口定义在调用方领域包中，具体 GORM 实现在 `controlplane/persistence`。
- 所有查询使用 `context.Context`。
- 数据库时间以 UTC 保存；成员有效性判断使用注入的 `Clock`，保证测试确定性。
- 创建和变更身份关系的内部应用服务使用显式事务和乐观并发版本。
- 首阶段公开 API 只有查询，因此不会产生不完整写入。
- SQLite 用于 repository 单元测试；MariaDB/MySQL 迁移是部署契约。测试必须覆盖两者共有的约束语义，不能依赖 SQLite 宽松行为掩盖缺失校验。

## 包结构

文件责任边界：

```text
backend/cmd/controlplane/                 # 独立进程入口和依赖装配
backend/cmd/controlplane-bootstrap/       # 仅空库可用的一次性初始化命令
backend/internal/controlplane/config/     # 新控制面配置
backend/internal/controlplane/authn/      # Principal、Authenticator、JWT 验证
backend/internal/controlplane/identity/   # User 领域与查询服务
backend/internal/controlplane/team/       # Team、Membership、角色与查询服务
backend/internal/controlplane/scope/      # RequestScope 和范围解析
backend/internal/controlplane/httpapi/    # Gin 路由、middleware、handler、envelope
backend/internal/controlplane/persistence/# 新 schema 的 GORM repositories
backend/internal/controlplane/testkit/    # 测试认证器、时钟和数据库夹具
backend/controlplane/migrations/          # 独立 SQL 迁移
```

每个包只暴露调用方需要的接口。`httpapi` 不直接访问 GORM；领域服务不导入 Gin；持久化不生成 HTTP 错误。

## 测试策略

实现严格采用测试驱动顺序。

### 领域测试

- 枚举只接受固定 TeamRole 和 PlatformRole。
- membership 生效、未来生效、过期、撤销的判定。
- suspended/disabled User 或 Team 不能形成 RequestScope。
- 平台角色不产生团队资格。

### 持久化测试

- User email 和 Team slug 唯一。
- Membership 外键和角色约束。
- 同一用户同一团队只能有一条 active membership。
- 有效团队列表过滤并稳定排序。
- 新 schema 可在空数据库创建，不引用旧表。

### HTTP 测试

- 缺少、格式错误、签名错误和过期 token 被拒绝。
- 未登记及非 active 用户被拒绝。
- 缺少、重复、非法的 active team header 被拒绝。
- 路径与 header 不一致被拒绝。
- 多团队用户只能按当前显式 team 获得对应 context。
- 无成员关系、过期成员关系和 suspended team 统一返回 `TEAM_ACCESS_DENIED`。
- 成功和错误响应都包含 request ID，且不泄露 token、数据库错误或其他团队资料。

### 进程级验证

- 编译独立 controlplane binary。
- bootstrap 命令可在空 schema 单事务创建首个治理主体，并在非空 schema 拒绝执行。
- 在只有新 schema 的空测试数据库启动。
- `/healthz` 存活，`/readyz` 在数据库可用时就绪。
- 全量新控制面测试可独立运行，不启动旧服务。

## 完成条件

本阶段只有在以下条件全部成立时完成：

1. 新控制面二进制不导入或调用旧业务包和旧服务。
2. 独立迁移可在空数据库建立 User、Team、TeamMembership 和 PlatformRoleBinding。
3. 一次性 bootstrap 只对全空身份 schema 生效，失败时不产生部分数据。
4. JWT 只产生身份主体；团队授权来自数据库当前状态。
5. `GET /v1/me` 和 `GET /v1/me/teams` 正确返回当前身份信息。
6. `GET /v1/teams/:team_id/context` 对缺失、错误、无权和跨团队范围安全拒绝。
7. 多团队测试证明一个请求只有一个团队 scope。
8. 平台角色与团队角色隔离测试通过。
9. 新控制面全部测试、构建和静态检查通过。

## 后续顺序

阶段 0 完成后，按最新控制面实现顺序继续：

1. 权威 Team/Membership 变更命令、Operation 最小状态和本地事务 Outbox。
2. RiskAssessment、Approval、Execution 的最小权威状态。
3. 命令 API、查询投影、审计写入和事件可靠性。
4. Service、Environment、TeamAsset 及第一个受控执行器适配器。

任何后续阶段都不得通过读取旧服务或旧表绕过新控制面的身份、范围、Operation 和事件不变量。
