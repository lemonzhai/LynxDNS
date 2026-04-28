# LynxDNS 内核说明文档

> **版本**: v1.1  
> **模块**: lynxdns-core  
> **语言**: Go 1.26+  
> **目标读者**: luci-app-lynxdns 前端开发团队  
> **更新说明**: 回归测试后更新，修复 30 个问题  

---

## 一、项目结构

```
lynxdns-core/
├── cmd/lynxdns/main.go          # 程序入口
├── internal/
│   ├── config/config.go         # 配置管理（YAML 加载/保存/热更新）
│   ├── log/log.go               # 日志系统（多级别/多输出/订阅推送）
│   ├── cache/cache.go           # DNS 缓存（Lazy Cache/淘汰策略）
│   ├── client/client.go         # 上游 DNS 客户端（UDP/TCP/DoT/DoH）
│   ├── server/server.go         # DNS 协议服务器（UDP/TCP 监听）
│   ├── rules/rules.go           # 规则匹配引擎（5种匹配模式）
│   ├── geodata/geodata.go       # GeoSite/GeoIP/广告过滤数据加载
│   ├── resolver/resolver.go     # DNS 解析引擎（分流逻辑核心）
│   ├── api/api.go               # RESTful API 服务器 + WebSocket
│   └── updater/updater.go       # 数据库自动更新（Cron 定时）
├── configs/config.yaml          # 默认配置文件
├── docs/
│   ├── LynxDNS_API_Protocol.md  # API 通讯协议文档
│   └── LynxDNS_Kernel_Guide.md  # 本文档
├── go.mod
└── go.sum
```

---

## 二、模块职责与接口

### 2.1 config — 配置管理

| 方法 | 说明 |
|------|------|
| `NewManager(path)` | 创建配置管理器 |
| `Load()` | 从文件加载配置 |
| `Save()` | 保存配置到文件 |
| `Get() *FullConfig` | 获取当前配置（只读） |
| `Update(partial map[string]interface{})` | 部分更新配置（热加载） |
| `Reload()` | 从文件重新加载 |
| `OnChange(fn func(*FullConfig))` | 注册配置变更回调 |

### 2.2 log — 日志管理

| 方法 | 说明 |
|------|------|
| `New(level)` | 创建 Logger |
| `Debug/Info/Warn/Error` | 分级日志输出 |
| `Subscribe() chan Entry` | 订阅日志流（用于 WebSocket 推送） |
| `Unsubscribe(ch)` | 取消订阅 |
| `AddOutput(w)` | 添加输出目标 |
| `SetLevel(level)` | 动态调整日志级别 |

### 2.3 cache — DNS 缓存

| 方法 | 说明 |
|------|------|
| `New(maxSize, lazy)` | 创建缓存（支持 Lazy 模式） |
| `Get(domain, qtype)` | 查询缓存 |
| `Set(domain, qtype, msg, serverUsed)` | 写入缓存 |
| `Delete(domain, qtype)` | 删除单条 |
| `Clear() int` | 清空全部，返回清除数量 |
| `Stats() Stats` | 获取命中率统计 |
| `Entries(page, pageSize)` | 分页获取缓存条目 |
| `IsLazy() bool` | 是否 Lazy Cache 模式 |
| `IsExpired(domain, qtype) bool` | 判断是否已过期 |

**Lazy Cache 行为**:
- 当 `lazy=true` 时，过期条目仍可返回，后台异步刷新
- 当 `lazy=false` 时，过期条目立即视为未命中
- **LRU 淘汰策略**：缓存满时淘汰最近最少使用的条目（使用 container/list 实现）

### 2.4 client — 上游 DNS 客户端

| 方法 | 说明 |
|------|------|
| `New(timeout)` | 创建客户端 |
| `AddUpstream(addr)` | 添加上游服务器 |
| `RemoveUpstream(addr)` | 移除上游服务器 |
| `Query(ctx, msg, addr) *Result` | 向指定服务器查询 |
| `QueryGroup(ctx, msg, servers, concurrency) *Result` | 并发查询多台服务器，返回最快响应 |
| `SetTimeout(timeout)` | 设置超时 |

**支持协议**:
- `udp://host:port` — 标准 UDP DNS
- `tcp://host:port` — TCP DNS
- `tls://host:port` — DNS over TLS (DoT)
- `https://url` — DNS over HTTPS (DoH)

**并发策略**: `QueryGroup` 同时向 N 台服务器发送查询（N 由 concurrency 参数控制），取最快成功响应返回。

### 2.5 server — DNS 协议服务器

| 方法 | 说明 |
|------|------|
| `New(addr, port, handler)` | 创建 DNS 服务器 |
| `Start()` | 启动 UDP + TCP 监听 |
| `Stop()` | 停止服务 |
| `IsRunning() bool` | 运行状态 |

`handler` 签名: `func(msg *dns.Msg, proto string, clientAddr net.Addr) *dns.Msg`

### 2.6 rules — 规则匹配引擎

| 方法 | 说明 |
|------|------|
| `NewEngine()` | 创建引擎 |
| `Add(rtype, domain, target, enabled) (*Rule, error)` | 添加规则 |
| `Update(id, rtype, domain, target, enabled) (*Rule, error)` | 更新规则 |
| `Delete(id) error` | 删除规则 |
| `Get(id) (*Rule, bool)` | 获取单条规则 |
| `List(rtype, enabledOnly) []*Rule` | 列出规则 |
| `Reorder(ids) error` | 重排优先级 |
| `Match(domain) *MatchResult` | 匹配域名 |

**规则类型**:
- `remote` — 使用远程 DNS 解析
- `domestic` — 使用国内 DNS 解析
- `redirect` — 重定向到指定 IP
- `block` — 拦截（返回空结果）

**匹配模式**（按 domain 字段格式自动识别）:
- `google.com` — 完全匹配
- `.google.com` — 域名后缀匹配（含自身）
- `*.google.com` — 通配符匹配
- `regexp:pattern` — 正则表达式匹配
- `keyword:text` — 关键词包含匹配

### 2.7 geodata — GeoSite/GeoIP 数据管理

| 方法 | 说明 |
|------|------|
| `NewManager(dataDir)` | 创建管理器 |
| `LoadGeosite(path) error` | 加载 GeoSite 数据（支持 protobuf 和文本格式） |
| `LoadGeoip(path) error` | 加载 GeoIP 数据（支持 protobuf 和文本格式） |
| `LoadAdFilter(path) error` | 加载广告过滤规则 |
| `LoadAll() error` | 加载全部数据 |
| `MatchGeosite(domain, categories) bool` | GeoSite 匹配 |
| `MatchAdFilter(domain) bool` | 广告域名匹配 |
| `GetGeositeStatus() GeoStatus` | GeoSite 状态 |
| `GetGeoipStatus() GeoStatus` | GeoIP 状态 |
| `GetAdFilterStatus() GeoStatus` | 广告过滤状态 |

**数据格式支持**:
- 自动检测 protobuf 二进制格式和文本格式
- 支持标准 geosite.dat/geoip.dat 文件（Loyalsoldier 等）
- 加载时自动计算 categories_count 和 entries_count

### 2.8 resolver — DNS 解析引擎（核心）

| 方法 | 说明 |
|------|------|
| `New(cfg, client, cache, rules, geo)` | 创建解析器 |
| `Resolve(msg, clientAddr) *dns.Msg` | 处理 DNS 请求（主入口） |
| `Lookup(ctx, domain, qtype) (*QueryLog, error)` | 手动解析（API 调用），返回 TTL 字段 |
| `UpdateConfig(cfg)` | 更新配置（自动去重上游服务器） |
| `GetStats() Stats` | 获取统计信息（含 queries_per_second、redirected_queries） |
| `Subscribe() chan QueryLog` | 订阅查询日志流 |
| `Unsubscribe(ch)` | 取消订阅 |
| `Uptime() time.Duration` | 运行时长 |

**DNS 防泄漏功能**:
- 配置项：`advanced.leak_protection.enabled` 和 `advanced.leak_protection.mode`
- 严格模式（`mode: strict`）：未匹配任何规则的域名返回 NXDOMAIN，防止 DNS 泄漏
- 宽松模式（默认）：未匹配的域名使用默认 DNS 解析

### 2.9 api — RESTful API 服务器

| 方法 | 说明 |
|------|------|
| `NewServer(cfg, resolver, rules, geo, cache, version)` | 创建 API 服务器 |
| `Start(addr, port)` | 启动 HTTP 服务 |
| `Stop(ctx)` | 停止服务 |

详见 [API 通讯协议文档](LynxDNS_API_Protocol.md)。

### 2.10 updater — 数据库自动更新

| 方法 | 说明 |
|------|------|
| `New(geoMgr, autoUpdate, cron, ...)` | 创建更新器 |
| `Start()` | 启动 Cron 定时任务 |
| `Stop()` | 停止 |
| `UpdateGeosite() error` | 更新 GeoSite |
| `UpdateGeoip() error` | 更新 GeoIP |
| `UpdateAdFilter() error` | 更新广告规则 |
| `UpdateAll() error` | 更新全部 |
| `UpdateTarget(target) error` | 按目标更新 |

---

## 三、DNS 解析流程

```
客户端 DNS 请求
    │
    ▼
┌──────────────┐
│  DNS Server  │  接收 UDP/TCP 请求
└──────┬───────┘
       │
       ▼
┌──────────────┐
│    Cache     │  查询缓存 → 命中则直接返回
└──────┬───────┘
       │ 未命中
       ▼
┌──────────────┐
│  自定义规则   │  最高优先级 → block/redirect/remote/domestic
└──────┬───────┘
       │ 未匹配
       ▼
┌──────────────┐
│  广告过滤     │  匹配广告域名 → 拦截
└──────┬───────┘
       │ 未匹配
       ▼
┌──────────────┐
│  GeoSite 匹配 │  匹配域名分类 → remote/domestic
└──────┬───────┘
       │ 未匹配
       ▼
┌──────────────┐
│  默认策略     │  使用默认 DNS（domestic）
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  DNS 防泄漏   │  严格模式下未匹配规则的域名返回 NXDOMAIN
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  DNS Client  │  向上游发送查询（支持并发）
└──────┬───────┘
       │
       ▼
┌──────────────┐
│  写入缓存     │  将结果缓存
└──────┬───────┘
       │
       ▼
    返回结果
```

---

## 四、编译与部署

### 4.1 编译

```bash
# 本地编译
go build -o lynxdns ./cmd/lynxdns

# 交叉编译（OpenWrt ARM64）
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o lynxdns-arm64 ./cmd/lynxdns

# 交叉编译（OpenWrt MIPS）
CGO_ENABLED=0 GOOS=linux GOARCH=mipsle go build -o lynxdns-mipsle ./cmd/lynxdns
```

### 4.2 运行

```bash
# 使用默认配置
./lynxdns -config /etc/lynxdns/config.yaml

# 查看版本
./lynxdns -version
```

### 4.3 配置文件

默认路径: `/etc/lynxdns/config.yaml`

详见 `configs/config.yaml` 示例配置。

---

## 五、测试

```bash
# 运行所有测试
go test ./... -v

# 运行指定模块测试
go test ./internal/config/ -v
go test ./internal/rules/ -v
go test ./internal/cache/ -v
go test ./internal/log/ -v
```

当前测试覆盖:
- config: 7 个测试（加载/保存/热更新/回调/YAML序列化）
- log: 5 个测试（级别/过滤/订阅/输出）
- rules: 17 个测试（精确/后缀/通配/正则/关键词/优先级/CRUD）
- cache: 11 个测试（读写/过期/Lazy/淘汰/统计/分页/大小写）

---

## 六、前端团队调用指南

### 6.1 API 通讯方式

前端通过 HTTP RESTful API 与内核交互：
- 基础地址: `http://127.0.0.1:9090/api/v1`
- 认证: `Authorization: Bearer <secret>`
- 实时数据: WebSocket `ws://127.0.0.1:9090/api/v1/stream/*`

### 6.2 快速上手

1. 获取配置: `GET /api/v1/config`
2. 修改配置: `PATCH /api/v1/config` (部分更新，自动热加载)
3. 查看状态: `GET /api/v1/status`
4. 管理规则: `GET/POST/PUT/DELETE /api/v1/rules`
5. 查看日志: WebSocket `/api/v1/stream/logs`

### 6.3 完整 API 文档

请参阅 [LynxDNS_API_Protocol.md](LynxDNS_API_Protocol.md) 获取完整的 API 端点、请求/响应示例和 LuaPP 集成代码。

---

> **文档结束** — LynxDNS 内核说明文档 v1.1
