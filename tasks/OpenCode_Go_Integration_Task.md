# 任务：在 CLIProxyAPI / CPAMC fork 中集成 OpenCode Go 额度查询与展示

## 一、项目背景

当前项目是 CLIProxyAPI / CPAMC 的 fork。项目已有以下相关页面：

1. **认证文件管理**
   - 用于集中管理 CLIProxy 支持的 JSON 认证文件或凭证。
   - 当前支持不同供应商，例如 Codex、Vertex、xAI 等。
   - 每个认证文件以卡片形式展示。
   - 卡片内已有供应商标签、启用状态、健康状态、成功/失败次数、底部操作按钮等。

2. **配额管理**
   - 用于集中查看 OAuth / 认证文件额度与剩余情况。
   - 当前已有 Claude、Antigravity、Codex 等额度分组。
   - 每个分组支持：按项显示、显示全部、刷新全部凭证。
   - 没有认证时会显示统一空状态。

**现在需要新增 OpenCode Go 额度查询功能**。

OpenCode Go 在 CLIProxyAPI 中是通过 OpenAI-compatible provider/upstream 接入的，实际模型请求仍然由现有 OpenAI-compatible 转发逻辑处理。本任务**不修改代理请求链路**，只新增 OpenCode Go 的认证凭证管理和官方额度展示。

## 二、核心目标

实现后，系统应当具备：

1. 在 **认证文件管理页面** 中新增 OpenCode Go 类型。
2. 用户可以添加 / 编辑 OpenCode Go 凭证。
3. OpenCode Go 凭证包含：
   - 账号名称
   - workspace_id / workspace 名称 / Default / workspace URL
   - auth cookie
   - 是否启用
   - 是否显示滚动用量
   - 是否显示每周用量
   - 是否显示每月用量
4. 在 **配额管理页面** 中新增 OpenCode Go 额度分组。
5. OpenCode Go 额度分组中展示每个 OpenCode Go 凭证的三个官方额度窗口：
   - 滚动用量 / 5 小时限额
   - 每周用量
   - 每月用量
6. 额度来源为 OpenCode 官方 Dashboard 页面中的官方计算结果。
7. **不根据 token 自行估算额度**。
8. **不实现通用 usage statistics**。
9. **不修改 OpenAI-compatible 请求转发逻辑**。
10. 尽量低侵入实现，降低未来同步上游 CLIProxyAPI 时的冲突风险。

## 三、重要设计原则

### 3.1 页面职责
必须遵循现有 CPAMC 信息架构：
- 认证文件管理 = 管理凭证
- 配额管理 = 查看额度
- AI 提供商 = 管理接入、路由、上游

因此：
- OpenCode Go 的 auth cookie 和 workspace 配置放在**认证文件管理**中。
- OpenCode Go 的 rolling / weekly / monthly 额度展示放在**配额管理**中。
- 不新增独立侧边栏页面。
- 不把三条额度直接硬塞进 AI 提供商页面。
- 不修改 OpenAI-compatible provider 的核心配置逻辑。

### 3.2 低侵入原则
实现时必须遵守：
1. 不修改核心代理请求链路。
2. 不修改 OpenAI-compatible 转发逻辑。
3. 不修改模型路由逻辑。
4. 不影响现有 Codex / xAI / Claude / Antigravity 额度功能。
5. 不把 OpenCode Go 逻辑写死在通用卡片组件中。
6. 优先通过 provider type / quota adapter / registry 形式接入。
7. 能新增文件就新增文件，少改现有大文件。
8. 修改现有文件时只做必要注册：
   - provider type 注册
   - tab/filter 注册
   - route 注册
   - quota group 注册
   - card renderer 注册
9. 上游同步时，冲突应尽量局限在少数注册文件中。

## 四、非目标

本次不要做以下内容：

1. 不做 token 级请求明细。
2. 不做模型消耗排行。
3. 不做成本估算。
4. 不做请求历史数据库。
5. 不做多用户计费系统。
6. 不做独立 OpenCode Go 大屏页面。
7. 不把 OpenCode Go 做成新的核心代理 provider。
8. 不修改 OpenCode Go 的 OpenAI-compatible 请求过程。
9. 不自动根据额度参与负载均衡。
10. 不把 auth cookie 暴露给前端。
11. 不保存 Dashboard HTML 原文。
12. 不在日志中输出 auth cookie。

## 五、参考实现

需要参考以下项目中 OpenCode Go 额度查询方式：

1. **Kiowx/opencode-cc**  
   仓库：https://github.com/Kiowx/opencode-cc  
   （将 OpenCode 转换成 Claude Code 兼容格式的高性能 API，包含相关额度跟踪逻辑）

2. **lvmiao233/QuotaHub**  
   仓库：https://github.com/lvmiao233/QuotaHub  
   （Coding plan quota hub，支持 OpenCode Go 的 rolling / weekly / monthly 额度查询与集中展示）

3. **Ruinique/opencode-go-dashboard**  
   仓库：https://github.com/Ruinique/opencode-go-dashboard  
   （自托管的 OpenCode Go 额度监控 Dashboard，支持多账号管理和额度刷新）

其中优先参考 **QuotaHub** 和 **opencode-cc**，因为它们支持通过 auth cookie 解析 workspace，不要求用户必须手动填写真实 `wrk_xxx`。

核心方式是：

```
auth cookie
  ↓
请求 OpenCode 内部 _server workspace function
  ↓
解析 workspace id
  ↓
请求 https://opencode.ai/workspace/{workspace_id}/go
  ↓
从 HTML / RSC 输出中解析 rollingUsage、weeklyUsage、monthlyUsage
  ↓
返回 usagePercent 和 resetInSec
```

## 六、OpenCode Go 凭证模型

### 6.1 新增认证类型
新增认证文件类型：`opencode_go`

展示名称：**OpenCode Go**  
短标签：**Go**（或 OpenCode）

### 6.2 凭证字段

**TypeScript 类型**：

```ts
type OpenCodeGoCredential = {
  id: string
  type: "opencode_go"
  name: string
  enabled: boolean

  workspace_id: string
  auth_cookie: string

  show_rolling: boolean
  show_weekly: boolean
  show_monthly: boolean

  refresh_interval_sec?: number

  created_at?: string
  updated_at?: string
}
```

**Go 结构体建议**：

```go
type OpenCodeGoCredential struct {
    ID                 string `json:"id"`
    Type               string `json:"type"`
    Name               string `json:"name"`
    Enabled            bool   `json:"enabled"`

    WorkspaceID        string `json:"workspace_id"`
    AuthCookie         string `json:"-"`                    // 后端保存用

    MaskedAuthCookie   string `json:"auth_cookie,omitempty"` // 前端显示用

    ShowRolling        bool   `json:"show_rolling"`
    ShowWeekly         bool   `json:"show_weekly"`
    ShowMonthly        bool   `json:"show_monthly"`

    RefreshIntervalSec int    `json:"refresh_interval_sec"`

    CreatedAt          string `json:"created_at,omitempty"`
    UpdatedAt          string `json:"updated_at,omitempty"`
}
```

**注意**：
- API 返回前端时**不能返回明文 auth_cookie**。
- 返回时只能返回 masked cookie。
- 保存时允许前端提交明文 cookie。
- 编辑时如果前端没有提交新 cookie，则保留原 cookie。

### 6.3 默认值
- `type`: `opencode_go`
- `enabled`: `true`
- `workspace_id`: `Default`
- `show_rolling`: `true`
- `show_weekly`: `true`
- `show_monthly`: `true`
- `refresh_interval_sec`: `60`（最小建议 15 秒）

## 七、认证文件管理页面改造

### 7.1 筛选 Tab
在认证文件页面的供应商筛选中新增 **OpenCode Go**（或短标签 **Go**）。

应和现有 Codex / Vertex / xAI 等筛选项保持一致（高度、圆角、badge 数字、active/hover 状态一致）。

### 7.2 认证文件卡片
OpenCode Go 卡片**复用现有认证文件卡片样式**。

卡片基础信息：
- [checkbox] [OpenCode Go icon] [OpenCode Go badge] [启用]
- 名称 / masked 信息
- 大小 / 修改时间
- 成功 0    失败 0
- 健康状态

**OpenCode Go 特有信息建议显示**：
- 工作区：`Default` / `wrk_xxx`
- 额度：前往配额管理查看

**不要在认证文件卡片里默认展示三条完整额度进度条**。

卡片底部操作按钮复用现有按钮：
`[模型] [设置] [删除]` + 启用 switch

可考虑新增弱入口：`[额度]`（点击跳转到配额管理页面对应分组）。

### 7.3 新增 / 编辑表单
表单字段：
- 名称
- Workspace ID / 名称（支持 `Default`、`工作区名称`、`wrk_xxx` 或包含 `wrk_xxx` 的 URL）
- Auth Cookie（password input）
- 显示滚动用量
- 显示每周用量
- 显示每月用量
- 刷新间隔
- 启用
- **测试额度查询** 按钮

**auth_cookie 字段要求**：
- 使用 password input。
- 保存后不显示明文。
- 如果已有 cookie，只显示 masked 状态。
- 提交时如果留空，不覆盖旧 cookie。
- 如果用户明确点击“清空”，才清除 cookie。

### 7.4 测试额度查询
表单中增加 **“测试额度查询”** 按钮。

点击后调用后端测试接口，不保存配置，只验证当前输入是否可用。

成功时显示：
```
测试成功
Workspace: wrk_xxx
滚动用量: 57%
每周用量: 23%
每月用量: 11%
```

失败时显示明确错误，例如：
- 认证失败，请检查 auth cookie 是否过期。
- 无法解析工作区，请检查 workspace 配置。
- 无法解析 OpenCode Go 额度数据，可能是 OpenCode Dashboard 页面结构已变更。

## 八、配额管理页面改造

### 8.1 新增额度分组
在配额管理页面新增 **OpenCode Go 额度** 分组（与 Claude / Antigravity / Codex 同级）。

标题区域复用现有布局：
`OpenCode Go 额度  2` + `[按项显示] [显示全部] [刷新全部凭证]`

### 8.2 空状态
如果没有 OpenCode Go 凭证，显示与现有分组一致的空状态文案：
> 暂无 OpenCode Go 认证  
> 添加 OpenCode Go 认证文件后即可查看额度。

### 8.3 OpenCode Go 额度卡片
每个 OpenCode Go 凭证对应一个额度卡片。

**样式要求**：
- 复用现有配额卡片样式（圆角、边框、阴影、间距与 Codex 一致）。
- badge 样式与现有供应商一致。
- progress bar 样式与现有额度一致。
- 不使用 OpenCode 官方页面的独立视觉风格。

**卡片建议结构**（展开模式）：
```
[OpenCode Go badge]  账号名称

工作区    Default / wrk_xxx
更新时间  2026/06/26 18:02:19

滚动用量                         57%   重置于 3 小时 32 分钟
[progress bar]

每周用量                         23%   重置于 2 天 13 小时
[progress bar]

每月用量                         11%   重置于 29 天 22 小时
[progress bar]

[刷新额度]
```

紧凑模式：
```
OpenCode Go    账号名称
滚动 57% · 每周 23% · 每月 11%
```

### 8.4 额度窗口
OpenCode Go 有三个窗口：
- `rolling` → 滚动用量（5 小时限额）
- `weekly` → 每周用量
- `monthly` → 每月用量

每个窗口显示：
- label
- used percent + progress bar
- remaining percent
- reset_in_sec formatted（相对时间）
- reset_at（绝对时间，可选）

### 8.5 颜色规则
优先复用现有额度颜色规则（success / warning / danger）。

### 8.6 错误状态
单个账号查询失败时，只影响该账号卡片，不影响其他账号。

错误卡片显示清晰提示，例如：
> 额度查询失败  
> 认证失败，请检查 auth cookie 是否过期。

## 九、后端 OpenCode Go Quota 模块

建议新增独立模块，尽量不侵入现有逻辑：

**推荐目录**：
```
internal/opencodegoquota/
├── types.go
├── cookie.go
├── workspace.go
├── parser.go
├── client.go
├── cache.go
```

或者按项目风格：
```
internal/quota/opencodego/
pkg/quota/opencodego/
```

**核心要求**：
- 不把 OpenCode Go 查询逻辑散落在 handler 或 UI API 中。
- 不写进 xAI / Codex 现有逻辑里。
- 不修改核心代理请求链路。

## 十、OpenCode Go 额度查询逻辑

### 10.1 Cookie 归一化
实现 `BuildOpenCodeGoCookieHeader(raw string)` 函数：
- 去除 `Cookie:` 前缀。
- 如果是裸 `Fe26...`，自动补成 `auth=Fe26...`。
- 如果包含多个 cookie，只提取 `auth`。
- 如果没有 auth，返回明确错误。

### 10.2 Cookie Mask
实现 `MaskOpenCodeGoCookie(raw string)`：
- `Fe26.abcdef1234567890` → `Fe26.abcd...7890`
- 过短则显示“已配置”

### 10.3 Workspace 解析
用户配置支持：
- `Default`
- `wrk_xxx`
- 工作区名称
- 包含 `wrk_xxx` 的 URL

解析优先级：
1. 直接匹配 `wrk_[A-Za-z0-9]+`
2. 请求 OpenCode workspace server function 获取列表
3. 按名称匹配
4. 空或 `Default` → 使用第一个 workspace

### 10.4 Dashboard 请求
请求：
```
GET https://opencode.ai/workspace/{workspace_id}/go
Cookie: auth=...
```

要求：
- 10 秒超时
- 不自动跟随重定向
- 最多读取 4MB
- 301/302/401/403/404 明确处理

### 10.5 HTML / RSC 解析
从 Dashboard HTML 中解析 `rollingUsage`、`weeklyUsage`、`monthlyUsage`。

每个对象提取 `usagePercent` 和 `resetInSec`。

实现 `ParseOpenCodeGoQuotaHTML(html string, now time.Time)`。

### 10.6 缓存
- 默认 TTL = `credential.refresh_interval_sec` 或 60 秒（最小 15 秒）
- 查询失败可缓存 15 秒
- 支持手动刷新绕过缓存
- 刷新全部凭证时限制并发（例如最多 3 个）

## 十一、后端 API 设计

优先复用现有认证文件和配额管理 API 风格。

### 11.1 认证文件 API
支持：
- 创建 / 编辑 / 删除 OpenCode Go 凭证
- 启用 / 禁用
- 测试额度查询（推荐 `POST /v0/management/auth-files/opencode-go/test`）

### 11.2 配额 API
推荐扩展统一配额接口，或新增：
- `GET /v0/management/quotas/opencode-go`
- `POST /v0/management/quotas/opencode-go/refresh`
- `POST /v0/management/quotas/opencode-go/{id}/refresh`

## 十二、前端架构

**不要硬编码**大量 `if (provider === "opencode_go")`。

推荐使用 provider config / renderer map 注册方式。

建议新增独立组件：
- `components/auth-files/OpenCodeGoCredentialForm.tsx`
- `components/quotas/OpenCodeGoQuotaCard.tsx`
- `components/quotas/QuotaWindowProgress.tsx`
- `utils/opencodeGoQuota.ts`

## 十三、安全要求

必须完成：
1. 明文 auth cookie 不返回前端。
2. 明文 auth cookie 不进入普通日志。
3. 错误信息中不能包含 auth cookie。
4. 前端 password input 不展示保存后的完整 cookie。
5. 测试接口和配额接口必须复用现有管理端鉴权。
6. 不保存 Dashboard HTML 原文。
7. 日志中最多输出 masked cookie 或 credential id。

## 十四、错误处理

后端错误类型建议：
- `opencode_go_auth_cookie_empty`
- `opencode_go_auth_failed`
- `opencode_go_workspace_lookup_failed`
- `opencode_go_dashboard_parse_failed`
- ...

前端中文文案建议见原文。

## 十五、实现步骤

1. 仓库调研（确认类型定义、API、UI 组件位置）
2. 新增 `opencode_go` 类型
3. 实现后端 quota client 模块
4. 接入认证文件 API（含 test）
5. 接入配额管理 API
6. 认证文件页面 UI（tab + 表单 + 卡片）
7. 配额管理页面 UI（分组 + 卡片）
8. 测试（单元测试 + 集成测试）
9. 构建与回归

## 十六、UI 细节规范

- 认证文件页面：视觉与 xAI / Codex 同级
- 配额管理页面：卡片样式与 Codex 一致
- 空状态、错误状态、紧凑/展开模式规范见原文

## 十七、验收标准

完成 23 条验收标准（详见原文），核心包括：
- 认证文件页面出现 OpenCode Go 筛选项
- 可新增/编辑/启用/禁用
- 可测试额度查询
- 配额管理页面出现 OpenCode Go 额度分组
- 正确显示三种窗口 + progress bar + reset 时间
- 不泄露 auth cookie
- 不影响现有功能和核心代理链路

## 十八、建议提交拆分

推荐按以下 commit 拆分：
1. `feat(auth): add opencode go credential type`
2. `feat(quota): add opencode go quota client`
3. `feat(api): expose opencode go credential test and quota endpoints`
4. `feat(ui): add opencode go credential form`
5. `feat(ui): add opencode go quota section`
6. `test(quota): add opencode go parser and cookie tests`
7. `docs: document opencode go quota setup`

## 十九、后续增强（不在本次实现）

- 保存额度快照
- 历史趋势图
- 对接 usage queue
- 模型消耗解释
- 多账号合并额度视图
- 额度不足时自动禁用凭证
- 根据额度参与负载均衡
- Webhook 通知
- Docker 独立部署 sidecar
- 支持更多 Dashboard 解析策略

---

**文件已生成**：`/home/workdir/artifacts/OpenCode_Go_Integration_Task.md`

你可以直接下载使用。需要我继续生成代码模板（parser、正则、cookie 处理函数等）吗？