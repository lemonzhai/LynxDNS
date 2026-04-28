# LynxDNS 核心功能测试问题清单

**项目**: LynxDNS 核心 (lynxdns-core)  
**测试日期**: 2026-04-19  
**问题总数**: 31 (原30 + 集成测试发现1)

---

## 问题统计

| 严重程度 | 数量 | 状态 |
|----------|------|------|
| 严重 (Critical) | 7 | ✅ 全部已修复 |
| 高危 (High) | 8 | ✅ 已修复 |
| 中危 (Medium) | 10 | ✅ 已修复 |
| 低危 (Low) | 6 | ✅ 已修复 |

---

## 一、严重问题 (Critical) - 必须立即修复

### CRIT-001: DNS 客户端 Query 方法死锁

| 属性 | 值 |
|------|-----|
| **文件** | internal/client/client.go |
| **行号** | 149-152 |
| **问题** | `Query` 方法中 `c.mu.RLock()` 调用了两次，导致死锁 |
| **影响** | 调用 Query 方法将导致程序永久阻塞 |
| **修复建议** | 将第二个 `c.mu.RLock()` 改为 `c.mu.RUnlock()` |

**问题代码**:
```go
func (c *DNSClient) Query(ctx context.Context, msg *dns.Msg, serverAddr string) *Result {
    c.mu.RLock()
    uc, ok := c.upstreams[serverAddr]
    c.mu.RLock()  // 错误：应为 c.mu.RUnlock()
```

---

### CRIT-002: DNS 服务器 WaitForShutdown 永久阻塞

| 属性 | 值 |
|------|-----|
| **文件** | internal/server/server.go |
| **行号** | 116-118 |
| **问题** | `stopCh` 通道从未被关闭，导致 `WaitForShutdown` 永久阻塞 |
| **影响** | 调用此方法将永久阻塞 |
| **修复建议** | 在 `Stop()` 方法中添加 `close(s.stopCh)` |

**问题代码**:
```go
func (s *DNSServer) WaitForShutdown(ctx context.Context) {
    <-s.stopCh  // stopCh 从未被关闭
}
```

---

### CRIT-003: GeoSite/GeoIP 数据解析格式不兼容

| 属性 | 值 |
|------|-----|
| **文件** | internal/geodata/geodata.go |
| **行号** | 283-330 |
| **问题** | `parseGeositeData()` 和 `parseGeoipData()` 使用简单文本格式解析，无法解析标准 protobuf 格式 |
| **影响** | 从 GitHub 下载的标准 geosite.dat/geoip.dat 文件无法正确加载 |
| **修复建议** | 实现标准 protobuf 格式解析 |

---

### CRIT-004: API restart 端点未实现实际重启

| 属性 | 值 |
|------|-----|
| **文件** | internal/api/api.go |
| **行号** | 176-179 |
| **问题** | `/api/v1/restart` 端点仅记录日志，未执行实际重启操作 |
| **影响** | 前端调用重启 API 无效 |
| **修复建议** | 实现服务重启逻辑（如调用系统重启或优雅重启机制） |

**问题代码**:
```go
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
    writeOK(w, nil)
    xlog.Info("restart requested via API")
    // 未实际执行重启操作
}
```

---

### CRIT-005: API geo/update 端点未实际触发更新

| 属性 | 值 |
|------|-----|
| **文件** | internal/api/api.go |
| **行号** | 374-387 |
| **问题** | `/api/v1/geo/update` 端点仅返回模拟响应，未调用 updater 执行更新 |
| **影响** | 前端调用更新 API 无效 |
| **修复建议** | 调用 `updater.UpdateTarget()` 执行实际更新 |

---

### CRIT-006: DNS 防泄漏功能实现无效

| 属性 | 值 |
|------|-----|
| **文件** | internal/resolver/resolver.go |
| **行号** | 227-231 |
| **问题** | `LeakProtection` 配置被读取但从未真正生效，两个分支执行完全相同的代码 |
| **影响** | DNS 防泄漏功能完全无效，用户配置被忽略 |
| **修复建议** | 实现真正的防泄漏逻辑：严格模式下未匹配规则的域名应被拒绝 |

**问题代码**:
```go
if cfg.Advanced.LeakProtection.Enabled && cfg.Advanced.LeakProtection.Mode == "strict" {
    return r.forwardToDefault(msg, domain, q, clientAddr)  // 与下面完全相同
}

return r.forwardToDefault(msg, domain, q, clientAddr)  // 无论配置如何都执行此行
```

**预期行为**:
- 严格模式下，未匹配任何规则的域名应被拒绝（返回 NXDOMAIN 或特定错误）
- 防止 DNS 查询泄漏到非预期的上游服务器

---

### CRIT-007: Lookup 方法未实现 DNS 防泄漏逻辑 (集成测试发现)

| 属性 | 值 |
|------|-----|
| **文件** | internal/resolver/resolver.go |
| **行号** | 340-363 |
| **问题** | `Lookup` 方法未检查 `LeakProtection` 配置，导致 API 调用时防泄漏功能无效 |
| **影响** | 通过 API 进行 DNS 解析时，防泄漏功能不生效 |
| **修复方式** | 在 Lookup 方法中添加完整的路由逻辑：广告过滤、Geosite 匹配、DNS 防泄漏检查 |
| **状态** | ✅ 已修复并验证 |

**修复验证结果**:
```json
{
  "domain": "nonexistent-test-domain-12345.com",
  "action": "blocked",
  "matched_rule": "leak_protection:strict",
  "ips": null
}
```

---

## 二、高危问题 (High) - 已修复

### HIGH-001: 缓存淘汰策略与文档不符

| 属性 | 值 |
|------|-----|
| **文件** | internal/cache/cache.go |
| **行号** | 214-226 |
| **问题** | 文档描述"LRU 淘汰策略"，实际实现为 TTL 优先策略 |
| **影响** | 功能与文档描述不一致 |
| **修复建议** | 统一文档和实现，或实现真正的 LRU 算法 |

---

### HIGH-002: 重定向响应只处理 A 记录

| 属性 | 值 |
|------|-----|
| **文件** | internal/resolver/resolver.go |
| **行号** | 420-432 |
| **问题** | `redirectResponse` 只创建 A 记录，AAAA 查询会返回错误响应 |
| **影响** | AAAA 记录查询被重定向时客户端可能解析失败 |
| **修复建议** | 根据查询类型返回对应记录类型 |

---

### HIGH-003: UpstreamStatuses 方法 context 资源泄露

| 属性 | 值 |
|------|-----|
| **文件** | internal/client/client.go |
| **行号** | 295-316 |
| **问题** | 循环内 defer 不会在每次迭代时执行，导致 context 资源泄露 |
| **影响** | 多个上游服务器时创建多个 context 但只在函数结束时释放 |
| **修复建议** | 在循环内立即调用 cancel() |

---

### HIGH-004: LookupResponse 字段命名不一致

| 属性 | 值 |
|------|-----|
| **文件** | internal/resolver/resolver.go |
| **行号** | 37 |
| **问题** | 文档定义字段名为 `ips`，实际命名为 `result_ips` |
| **影响** | 前端解析可能出错 |
| **修复建议** | 将 `ResultIPs` 字段 JSON tag 改为 `ips` |

---

### HIGH-005: LookupResponse 缺失 TTL 字段

| 属性 | 值 |
|------|-----|
| **文件** | internal/resolver/resolver.go |
| **行号** | 31-42 |
| **问题** | 文档定义 `ttl` 字段，实际未实现 |
| **影响** | 前端无法获取 TTL 信息 |
| **修复建议** | 添加 TTL 字段并在 Lookup 中填充 |

---

### HIGH-006: Status API 缺失关键字段

| 属性 | 值 |
|------|-----|
| **文件** | internal/api/api.go |
| **行号** | 162-174 |
| **问题** | 缺失 `goroutines`、`memory_mb`、`dns_servers` 字段 |
| **影响** | 前端无法获取完整状态信息 |
| **修复建议** | 使用 `runtime.NumGoroutine()` 和 `runtime.ReadMemStats()` |

---

### HIGH-007: DNS Stats API 缺失字段

| 属性 | 值 |
|------|-----|
| **文件** | internal/resolver/resolver.go |
| **行号** | 44-53 |
| **问题** | 缺失 `queries_per_second`、`redirected_queries` 字段 |
| **影响** | 前端无法获取完整统计信息 |
| **修复建议** | 添加相关字段并实现计算逻辑 |

---

### HIGH-008: WebSocket 日志级别过滤未实现

| 属性 | 值 |
|------|-----|
| **文件** | internal/api/api.go |
| **行号** | 401-428 |
| **问题** | 文档定义 `level` 查询参数，实际未实现过滤功能 |
| **影响** | 无法按级别过滤日志流 |
| **修复建议** | 解析 `level` 参数并在推送前过滤 |

---

## 三、中危问题 (Medium) - 建议修复

### MED-001: 配置 Get 方法返回可修改指针

| 属性 | 值 |
|------|-----|
| **文件** | internal/config/config.go |
| **行号** | 186-190 |
| **问题** | 返回指针非只读副本，违反"只读"语义 |
| **影响** | 外部代码可直接修改配置对象 |
| **修复建议** | 返回配置的深拷贝 |

---

### MED-002: IsExpired 方法语义有歧义

| 属性 | 值 |
|------|-----|
| **文件** | internal/cache/cache.go |
| **行号** | 113-123 |
| **问题** | 条目不存在时返回 false，语义不明确 |
| **影响** | 调用者无法区分"未过期"和"不存在" |
| **修复建议** | 返回错误或单独的状态 |

---

### MED-003: 规则 Update 方法 enabled 参数处理不当

| 属性 | 值 |
|------|-----|
| **文件** | internal/rules/rules.go |
| **行号** | 89-115 |
| **问题** | 总是设置 enabled，无法保持原值 |
| **影响** | 无法只更新部分字段 |
| **修复建议** | 添加指针参数或使用可选结构体 |

---

### MED-004: ensurePort 函数实现无效

| 属性 | 值 |
|------|-----|
| **文件** | internal/client/client.go |
| **行号** | 318-326 |
| **问题** | 函数名称暗示会确保端口存在，但实际实现无效 |
| **影响** | 可能导致地址解析问题 |
| **修复建议** | 正确实现端口添加逻辑 |

---

### MED-005: initUpstreams 方法重复添加上游服务器

| 属性 | 值 |
|------|-----|
| **文件** | internal/resolver/resolver.go |
| **行号** | 97-110 |
| **问题** | 未检查是否已存在，多次调用会重复添加 |
| **影响** | 配置更新后上游服务器列表重复 |
| **修复建议** | 在 AddUpstream 时检查是否已存在 |

---

### MED-006: UpdateAll 方法错误处理不当

| 属性 | 值 |
|------|-----|
| **文件** | internal/updater/updater.go |
| **行号** | 77-91 |
| **问题** | 即使所有更新都失败也返回 nil |
| **影响** | 调用者无法知道更新是否成功 |
| **修复建议** | 使用 `errors.Join` 返回聚合错误 |

---

### MED-007: 缓存域名筛选参数未实现

| 属性 | 值 |
|------|-----|
| **文件** | internal/api/api.go |
| **行号** | 217-238 |
| **问题** | 文档定义 `domain` 查询参数，实际未实现 |
| **影响** | 无法按域名筛选缓存条目 |
| **修复建议** | 实现域名筛选逻辑 |

---

### MED-008: 规则启用状态筛选参数未实现

| 属性 | 值 |
|------|-----|
| **文件** | internal/api/api.go |
| **行号** | 275-285 |
| **问题** | 文档定义 `enabled` 查询参数，实际未实现 |
| **影响** | 无法按启用状态筛选规则 |
| **修复建议** | 实现启用状态筛选逻辑 |

---

### MED-009: GeoStatus 缺失字段

| 属性 | 值 |
|------|-----|
| **文件** | internal/geodata/geodata.go |
| **行号** | 14-23 |
| **问题** | 缺失 `categories_count`、`entries_count` 字段 |
| **影响** | 前端无法获取完整状态信息 |
| **修复建议** | 在加载时计算并存储这些值 |

---

### MED-010: API 响应 message 与文档不一致

| 属性 | 值 |
|------|-----|
| **文件** | internal/api/api.go |
| **行号** | 多处 |
| **问题** | 多个端点响应 message 与文档定义不一致 |
| **影响** | 前端依赖 message 进行判断时可能出错 |
| **修复建议** | 统一所有端点的 message 与文档一致 |

**涉及端点**:
- PATCH /api/v1/config
- POST /api/v1/config/reload
- DELETE /api/v1/dns/cache
- PUT /api/v1/rules/:id
- DELETE /api/v1/rules/:id
- POST /api/v1/rules/order
- POST /api/v1/geo/update

---

## 四、低危问题 (Low) - 可选修复

### LOW-001: 版本信息硬编码

| 属性 | 值 |
|------|-----|
| **文件** | internal/api/api.go |
| **行号** | 152-159 |
| **问题** | `build_time`、`go_version`、`os`、`arch` 均为硬编码值 |
| **影响** | 版本信息不准确 |
| **修复建议** | 使用 `-ldflags` 在编译时注入实际值 |

---

### LOW-002: dnsStringToType 使用硬编码值

| 属性 | 值 |
|------|-----|
| **文件** | internal/api/api.go |
| **行号** | 481-505 |
| **问题** | 使用硬编码数字而非常量 |
| **影响** | 代码可读性差 |
| **修复建议** | 使用 `dns.TypeA`、`dns.TypeAAAA` 等常量 |

---

### LOW-003: 默认 Logger 未初始化 subscribers

| 属性 | 值 |
|------|-----|
| **文件** | internal/log/log.go |
| **行号** | 63-70 |
| **问题** | `subscribers` 切片未初始化 |
| **影响** | 使用包级 Subscribe() 时可能 panic |
| **修复建议** | 初始化 subscribers 为空切片 |

---

### LOW-004: LoadFromConfig 缺少 enabled 字段支持

| 属性 | 值 |
|------|-----|
| **文件** | internal/rules/rules.go |
| **行号** | 268-280 |
| **问题** | enabled 硬编码为 true |
| **影响** | 从配置加载规则时无法指定 enabled 状态 |
| **修复建议** | 在配置结构体中添加 enabled 字段 |

---

### LOW-005: 409 冲突错误码未实现

| 属性 | 值 |
|------|-----|
| **文件** | internal/api/api.go |
| **行号** | - |
| **问题** | 文档定义 409 用于规则重复场景，实际未实现 |
| **影响** | 规则重复时返回错误码不符合文档 |
| **修复建议** | 添加规则重复检测并返回 409 |

---

### LOW-006: WebSocket 日志 level 字段序列化为整数

| 属性 | 值 |
|------|-----|
| **文件** | internal/log/log.go |
| **行号** | 50-54 |
| **问题** | Level 类型序列化为整数而非字符串 |
| **影响** | 与文档定义的字符串格式不一致 |
| **修复建议** | 实现 `MarshalJSON` 方法序列化为字符串 |

---

## 五、缺失功能

| 编号 | 功能 | 文档位置 | 优先级 |
|------|------|----------|--------|
| MISS-001 | 服务重启逻辑 | POST /api/v1/restart | 高 |
| MISS-002 | Geo 数据更新触发 | POST /api/v1/geo/update | 高 |
| MISS-003 | GeoIP 匹配功能 | MatchGeoip() | 中 |
| MISS-004 | Bootstrap DNS 使用 | 配置中有但未使用 | 低 |
| MISS-005 | 日志文件输出 | Log.File 配置 | 低 |
| MISS-006 | 上游服务器状态监控 | dns_servers 字段 | 中 |

---

## 六、测试覆盖缺失

| 模块 | 测试文件 | 状态 |
|------|----------|------|
| client | 无 | 需添加 |
| server | 无 | 需添加 |
| geodata | 无 | 需添加 |
| resolver | 无 | 需添加 |
| api | 无 | 需添加 |
| updater | 无 | 需添加 |

---

**清单生成时间**: 2026-04-19  
**清单版本**: v1.0
