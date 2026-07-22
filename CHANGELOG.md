# 更新日志

所有重要变更均记录于此文件。格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

---

## [1.0.3] - 2026-07-22

本次更新聚焦于 **DNS 查询可靠性**、**上游服务器可观测性** 与 **运行状态页监控能力增强**，并修复了若干统计与展示不一致问题。

### 🚀 新增功能

#### 1. 上游 DNS 服务器监控增强（核心新特性）

为运行状态页带来了完整的按上游维度的统计与熔断能力，此前内核几乎不记录任何按上游服务器的统计指标。

**新建独立模块 `internal/upstream/`**（熔断与统计解耦，便于后续 DoH/DoQ 等新协议复用）

- `stats.go` — 原子计数 + 1024 环形缓冲延迟采样 + P95/P99 分位数计算（nearest rank 算法）
- `breaker.go` — 熔断状态机：`closed` → `open` → `half_open` → `closed`/`open`

**新增统计指标（每个上游服务器维度）**

| 指标 | 说明 |
|------|------|
| 请求次数 | 看负载分布 |
| 成功率 / 失败次数 | 判断上游稳定性 |
| 超时次数 | 判断链路质量 |
| P95 / P99 延迟 | 看尾延迟，避免平均值掩盖偶发慢请求 |
| 当前熔断状态 | `closed` / `open` / `half_open`，运维直观可见 |
| 熔断次数 / 恢复次数 | 判断健康检查是否频繁切换 |

**熔断策略**：连续失败 5 次触发熔断 → 冷却 10 秒后进入半开状态放行一次探针 → 探针成功则恢复，失败则重新熔断。

**运行状态页（overview.js）增强**

- 顶部统计卡片新增「成功率」「超时率」，共 8 张卡片
- DNS 服务器状态表重构为 11 列：所属组 / 地址 / 协议 / 状态 / 请求 / 成功率 / P95 / P99 / 超时 / 当前状态 / 熔断·恢复
- 当前熔断状态用绿色（正常）/ 红色（熔断）/ 橙色（半开）彩色标签区分
- 表格支持横向滚动，适配窄屏

#### 2. 苹果域名分流规则优化

默认国内分流配置新增 `apple-cn` 分类，确保 App Store、iCloud 等 Apple 相关域名优先使用国内 DNS 服务器解析，提升访问速度与稳定性。

#### 3. 配置热更新

修改配置无需重启服务，运行中即可生效。

#### 4. 日志轮转功能

支持日志文件自动轮转，避免日志文件无限增长占用磁盘。

#### 5. Geo 数据更新备份 URL

支持 GeoSite/GeoIP/广告过滤规则的主源 + 备用源列表，多源故障转移，提升数据更新可靠性。

### ⚡ 性能与稳定性改进

#### DNS 查询数据包大小优化（EDNS0）

- 设置合理的 EDNS0 数据包大小配置：
  - `DefaultEDNSSize = 1232`：默认 UDP 数据包大小，在大多数网络环境中稳定
  - `MaxEDNSSize = 4096`：最大支持的数据包大小
- 服务器端实现智能 UDP 响应截断机制：
  - 尊重客户端在 EDNS0 选项中声明的最大支持大小
  - 响应超过客户端支持大小时自动截断并设置 TC（截断）标志
  - 确保不同网络环境下 DNS 查询的可靠性

#### 跨组地址去重展示

DNS 服务器状态表对同一物理地址在多个组（如「国内」与「引导」）中的重复展示进行去重，合并显示「所属组」列（如「国内 / 引导」）。此前同一地址会在不同组各渲染一行且数据相同，易引起「数据被重复计算」的误解。

**说明**：内核按地址去重是正确设计（同一物理服务器共享一套统计/熔断实例，避免状态分裂），统计仅累加一次。本次仅优化展示层。

#### API 向后兼容

- 旧字段（`status`、`avg_latency_ms`）全部保留
- 新增字段（`requests`、`successes`、`failures`、`timeouts`、`p95_latency_ms`、`p99_latency_ms`、`trip_count`、`recover_count`、`current_state`、`protocol`）追加扩展
- 旧版本前端无需修改即可继续使用

### 🐛 问题修复

- **修复 `by_action["failed"]` 计数缺失**：`resolver.forwardToGroup` 失败分支此前未将 `ActionFailed` 计入 `by_action` map，导致该常量定义了却从未生效，与前端 `failed: '超时'` 标签不一致。现已在失败分支补齐计数。
- **补齐 `bootstrap` 组状态展示**：`/status` 端点的 `dns_servers` 此前仅展示 `domestic` / `remote` 两组，未展示 `bootstrap` 引导 DNS。现已补齐，引导组上游状态可见。

### ⚙️ 内部优化

- **统计与业务逻辑解耦**：统计方法以 `defer recover()` 兜底，绝不阻断 DNS 请求；统计 Mutex 与 DNS 请求 RLock 分离，不争用。
- **不引入第三方分位数库**：采用环形缓冲 + 排序方案自实现 P95/P99，保持 OpenWrt 交叉编译友好（OpenWrt 路由器 QPS 通常 <100，Mutex 开销可忽略）。
- **不引入后台健康检查 goroutine**：保持即时探测模型，降低改动面。
- **更新 API 协议文档**：`docs/LynxDNS_API_Protocol.md` 中 `ServerStatus` 结构体与响应示例已同步。

### 📦 升级与兼容

- **完全向后兼容**：本次更新不删除旧字段、不修改旧字段含义，仅追加新字段扩展
- **旧版本前端**：无需修改即可继续工作（新字段会被忽略）
- **建议升级**：运行状态页的完整监控能力需前后端同步升级后体验

### 📝 涉及的主要文件

**核心引擎（lynxdns-core）**

- 新建 `internal/upstream/stats.go`、`internal/upstream/breaker.go`
- 修改 `internal/client/client.go`、`internal/api/api.go`、`internal/resolver/resolver.go`、`internal/server/server.go`、`internal/config/config.go`、`internal/log/log.go`
- 更新 `docs/LynxDNS_API_Protocol.md`

**Web 管理界面（luci-app-lynxdns）**

- 修改 `htdocs/luci-static/resources/view/lynxdns/overview.js`

---

## 版本规范

基于 [语义化版本](https://semver.org/lang/zh-CN/)。
