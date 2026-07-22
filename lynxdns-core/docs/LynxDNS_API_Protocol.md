# LynxDNS LuaPP 界面通讯协议设计文档

> **版本**: v1.2  
> **日期**: 2026-04-19  
> **更新说明**: 新增多源故障转移、下载进度反馈、更新历史记录 API  
> **适用对象**: luci-app-lynxdns 前端开发团队  
> **内核模块**: lynxdns-core  

---

## 一、概述

LynxDNS 内核采用 **RESTful API + WebSocket 双通道通讯架构**，内核运行后启动 HTTP API Server，前端（luci-app-lynxdns）通过 HTTP 请求进行配置管理和状态查询，通过 WebSocket 接收实时日志和 DNS 查询流。

| 项目 | 默认值 |
|------|--------|
| API 监听地址 | `127.0.0.1` |
| API 监听端口 | `9090` |
| 认证方式 | Bearer Token |
| 数据格式 | JSON |
| 配置文件 | `/etc/lynxdns/config.yaml` |

---

## 二、通讯架构

```
┌─────────────────┐         ┌──────────────────────────────┐
│  luci-app       │  HTTP   │  LynxDNS Core                │
│  (LuCI前端)     │◄───────►│                              │
│                 │  REST   │  ┌────────────────────────┐  │
│                 │         │  │  API Server (Chi)      │  │
│                 │  WebSocket │  │  ├─ /api/v1/*        │  │
│                 │◄───────►│  │  ├─ /api/v1/stream/*  │  │
│                 │   WS    │  └────────────────────────┘  │
└─────────────────┘         │                              │
                            │  ┌────────────────────────┐  │
                            │  │  DNS Engine            │  │
                            │  │  ├─ Resolver           │  │
                            │  │  ├─ Rule Engine        │  │
                            │  │  ├─ Cache              │  │
                            │  │  └─ GeoData            │  │
                            │  └────────────────────────┘  │
                            └──────────────────────────────┘
```

### 2.1 HTTP RESTful API

- **用途**: 配置管理、状态查询、操作指令
- **Content-Type**: `application/json`
- **认证头**: `Authorization: Bearer <secret>`

### 2.2 WebSocket 实时流

- **用途**: 实时日志推送、实时 DNS 查询流
- **连接地址**: `ws://<host>:<port>/api/v1/stream/<stream_type>`
- **认证**: 连接时在 URL 中携带 `?token=<secret>` 或通过首条消息发送 token

---

## 三、统一响应格式

### 3.1 成功响应

```json
{
  "code": 0,
  "message": "success",
  "data": { }
}
```

### 3.2 错误响应

```json
{
  "code": 400,
  "message": "invalid parameter: domain is required",
  "data": null
}
```

### 3.3 错误码定义

| 错误码 | 含义 | 说明 |
|--------|------|------|
| 0 | 成功 | 请求处理成功 |
| 400 | 请求错误 | 参数缺失或格式错误 |
| 401 | 未授权 | Token 缺失或无效 |
| 404 | 未找到 | 资源不存在 |
| 409 | 冲突 | 资源已存在或状态冲突（如规则重复） |
| 500 | 内部错误 | 服务器内部错误 |

---

## 四、完整 API 端点列表

### 4.1 系统管理

#### 4.1.1 获取版本信息

```
GET /api/v1/version
```

**响应示例**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "version": "1.0.0",
    "build_time": "2026-04-19T12:00:00Z",
    "go_version": "go1.26.2",
    "os": "linux",
    "arch": "amd64"
  }
}
```

**curl 示例**:

```bash
curl -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/version
```

---

#### 4.1.2 获取服务状态

```
GET /api/v1/status
```

**响应示例**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "running": true,
    "uptime_seconds": 86400,
    "dns_queries_total": 15234,
    "cache_hits": 8912,
    "cache_hit_rate": 58.5,
    "avg_latency_ms": 12.3,
    "goroutines": 24,
    "memory_mb": 18.5,
    "dns_servers": {
      "domestic": [
        {
          "address": "udp://223.5.5.5:53",
          "status": "up",
          "avg_latency_ms": 5.2,
          "requests": 10234,
          "successes": 10230,
          "failures": 4,
          "timeouts": 2,
          "p95_latency_ms": 12.5,
          "p99_latency_ms": 18.3,
          "avg_latency_ms_hist": 5.1,
          "current_state": "closed",
          "trip_count": 0,
          "recover_count": 0,
          "protocol": "udp"
        },
        {"address": "udp://119.29.29.29:53", "status": "up", "avg_latency_ms": 4.8}
      ],
      "remote": [
        {"address": "tls://1.1.1.1:853", "status": "up", "avg_latency_ms": 45.1},
        {"address": "https://dns.google/dns-query", "status": "down", "avg_latency_ms": 0}
      ],
      "bootstrap": [
        {"address": "udp://223.5.5.5:53", "status": "up", "avg_latency_ms": 5.0}
      ]
    }
  }
}
```

**curl 示例**:

```bash
curl -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/status
```

---

#### 4.1.3 重启服务

```
POST /api/v1/restart
```

**请求体**: 无

**响应示例**:

```json
{
  "code": 0,
  "message": "service restarting",
  "data": null
}
```

**curl 示例**:

```bash
curl -X POST -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/restart
```

---

### 4.2 配置管理

#### 4.2.1 获取完整配置

```
GET /api/v1/config
```

**响应示例**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "server": {
      "enabled": true,
      "listen_addr": "0.0.0.0",
      "listen_port": 5353
    },
    "api": {
      "addr": "127.0.0.1",
      "port": 9090,
      "secret": "******"
    },
    "dns": {
      "domestic": ["udp://223.5.5.5:53", "udp://119.29.29.29:53"],
      "remote": ["tls://1.1.1.1:853", "https://dns.google/dns-query"],
      "default": "domestic",
      "bootstrap": ["udp://223.5.5.5:53", "udp://119.29.29.29:53"]
    },
    "advanced": {
      "concurrency": 3,
      "idle_timeout": 30,
      "cache": {
        "enabled": true,
        "size": 4096,
        "lazy": true
      },
      "leak_protection": {
        "enabled": true,
        "mode": "strict"
      }
    },
    "geo": {
      "auto_update": true,
      "update_cron": "0 3 * * *",
      "geosite_url": "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat",
      "geoip_url": "https://github.com/Loyalsoldier/geoip/releases/latest/download/geoip.dat",
      "ad_filter_url": "https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt",
      "data_dir": "/etc/lynxdns/data"
    },
    "routing": {
      "geosite": {
        "remote": ["geolocation-!cn", "google", "github", "gfw"],
        "domestic": ["cn"],
        "ad_filter": ["category-ads-all"]
      },
      "geoip": {
        "remote": ["!cn"]
      }
    },
    "log": {
      "level": "info",
      "file": "/var/log/lynxdns.log"
    }
  }
}
```

**curl 示例**:

```bash
curl -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/config
```

---

#### 4.2.2 部分更新配置（热加载）

```
PATCH /api/v1/config
```

**请求体** (仅包含需要更新的字段):

```json
{
  "server": {
    "listen_port": 5354
  },
  "advanced": {
    "cache": {
      "size": 8192
    }
  }
}
```

**响应示例**:

```json
{
  "code": 0,
  "message": "config updated and hot-reloaded",
  "data": null
}
```

**说明**: 
- 支持部分更新，无需提交完整配置
- 更新后自动热加载，无需重启服务
- 内核会自动校验配置合法性

**curl 示例**:

```bash
curl -X PATCH -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"server":{"listen_port":5354}}' \
  http://127.0.0.1:9090/api/v1/config
```

---

#### 4.2.3 重新加载配置文件

```
POST /api/v1/config/reload
```

**请求体**: 无

**响应示例**:

```json
{
  "code": 0,
  "message": "config reloaded from file",
  "data": null
}
```

**curl 示例**:

```bash
curl -X POST -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/config/reload
```

---

### 4.3 DNS 管理

#### 4.3.1 获取 DNS 统计信息

```
GET /api/v1/dns/stats
```

**响应示例**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total_queries": 15234,
    "cache_hits": 8912,
    "cache_hit_rate": 58.5,
    "avg_latency_ms": 12.3,
    "queries_per_second": 2.5,
    "blocked_queries": 156,
    "redirected_queries": 23,
    "by_type": {
      "A": 12000,
      "AAAA": 2500,
      "MX": 234,
      "TXT": 100,
      "CNAME": 400
    },
    "by_status": {
      "success": 14500,
      "failed": 534,
      "timeout": 200
    },
    "by_action": {
      "domestic": 8500,
      "remote": 5000,
      "blocked": 156,
      "redirected": 23,
      "default": 1555
    }
  }
}
```

**curl 示例**:

```bash
curl -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/dns/stats
```

---

#### 4.3.2 获取缓存信息

```
GET /api/v1/dns/cache
```

**查询参数**:

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| page | int | 1 | 页码 |
| page_size | int | 20 | 每页条数 |
| domain | string | "" | 按域名筛选 |

**响应示例**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total_entries": 3200,
    "max_size": 4096,
    "total": 3200,
    "page": 1,
    "page_size": 20,
    "entries": [
      {
        "domain": "www.baidu.com",
        "type": "A",
        "ttl": 300,
        "remaining_ttl": 245,
        "expire_at": "2026-04-19T13:00:00Z",
        "value": ["110.242.68.66", "110.242.68.67"],
        "server_used": "udp://223.5.5.5:53"
      }
    ]
  }
}
```

**curl 示例**:

```bash
curl -H "Authorization: Bearer your-secret-key" "http://127.0.0.1:9090/api/v1/dns/cache?page=1&page_size=20"
```

---

#### 4.3.3 清除 DNS 缓存

```
DELETE /api/v1/dns/cache
```

**响应示例**:

```json
{
  "code": 0,
  "message": "cache cleared",
  "data": {
    "cleared_entries": 3200
  }
}
```

**curl 示例**:

```bash
curl -X DELETE -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/dns/cache
```

---

#### 4.3.4 手动解析域名

```
POST /api/v1/dns/lookup
```

**请求体**:

```json
{
  "domain": "www.google.com",
  "type": "A"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| domain | string | 是 | 要解析的域名 |
| type | string | 否 | 记录类型，默认 A |

**响应示例**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "domain": "www.google.com",
    "type": "A",
    "ips": ["142.250.80.46"],
    "ttl": 300,
    "server_used": "tls://1.1.1.1:853",
    "latency_ms": 45.2,
    "action": "remote",
    "matched_rule": "geosite:geolocation-!cn"
  }
}
```

**curl 示例**:

```bash
curl -X POST -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"domain":"www.google.com","type":"A"}' \
  http://127.0.0.1:9090/api/v1/dns/lookup
```

---

### 4.4 规则管理

#### 4.4.1 获取所有规则

```
GET /api/v1/rules
```

**查询参数**:

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| type | string | "" | 按类型筛选：remote/domestic/redirect/block |
| enabled | bool | - | 按启用状态筛选 |

**响应示例**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 5,
    "rules": [
      {
        "id": "rule_001",
        "type": "remote",
        "domain": ".google.com",
        "target": "",
        "enabled": true,
        "created_at": "2026-04-19T12:00:00Z",
        "updated_at": "2026-04-19T12:00:00Z"
      },
      {
        "id": "rule_002",
        "type": "domestic",
        "domain": ".baidu.com",
        "target": "",
        "enabled": true,
        "created_at": "2026-04-19T12:00:00Z",
        "updated_at": "2026-04-19T12:00:00Z"
      },
      {
        "id": "rule_003",
        "type": "redirect",
        "domain": "example.local",
        "target": "192.168.1.100",
        "enabled": true,
        "created_at": "2026-04-19T12:00:00Z",
        "updated_at": "2026-04-19T12:00:00Z"
      },
      {
        "id": "rule_004",
        "type": "block",
        "domain": ".ad.example.com",
        "target": "",
        "enabled": true,
        "created_at": "2026-04-19T12:00:00Z",
        "updated_at": "2026-04-19T12:00:00Z"
      },
      {
        "id": "rule_005",
        "type": "remote",
        "domain": "regexp:^ads.*\\.com$",
        "target": "",
        "enabled": false,
        "created_at": "2026-04-19T12:00:00Z",
        "updated_at": "2026-04-19T12:00:00Z"
      }
    ]
  }
}
```

**curl 示例**:

```bash
curl -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/rules
curl -H "Authorization: Bearer your-secret-key" "http://127.0.0.1:9090/api/v1/rules?type=remote"
```

---

#### 4.4.2 添加规则

```
POST /api/v1/rules
```

**请求体**:

```json
{
  "type": "remote",
  "domain": ".github.com",
  "target": "",
  "enabled": true
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| type | string | 是 | 规则类型：remote / domestic / redirect / block |
| domain | string | 是 | 域名匹配规则（支持完全匹配、后缀 .xxx、通配符 *.xxx、正则 regexp:、关键词 keyword:） |
| target | string | 条件必填 | 重定向目标 IP（仅 redirect 类型必填） |
| enabled | bool | 否 | 是否启用，默认 true |

**响应示例**:

```json
{
  "code": 0,
  "message": "rule added",
  "data": {
    "id": "rule_006",
    "type": "remote",
    "domain": ".github.com",
    "target": "",
    "enabled": true,
    "created_at": "2026-04-19T13:00:00Z",
    "updated_at": "2026-04-19T13:00:00Z"
  }
}
```

**curl 示例**:

```bash
curl -X POST -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"type":"remote","domain":".github.com","enabled":true}' \
  http://127.0.0.1:9090/api/v1/rules
```

---

#### 4.4.3 更新规则

```
PUT /api/v1/rules/:id
```

**请求体**:

```json
{
  "domain": ".githubusercontent.com",
  "enabled": true
}
```

**响应示例**:

```json
{
  "code": 0,
  "message": "rule updated",
  "data": {
    "id": "rule_006",
    "type": "remote",
    "domain": ".githubusercontent.com",
    "target": "",
    "enabled": true,
    "created_at": "2026-04-19T13:00:00Z",
    "updated_at": "2026-04-19T13:05:00Z"
  }
}
```

**curl 示例**:

```bash
curl -X PUT -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"domain":".githubusercontent.com","enabled":true}' \
  http://127.0.0.1:9090/api/v1/rules/rule_006
```

---

#### 4.4.4 删除规则

```
DELETE /api/v1/rules/:id
```

**响应示例**:

```json
{
  "code": 0,
  "message": "rule deleted",
  "data": null
}
```

**curl 示例**:

```bash
curl -X DELETE -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/rules/rule_006
```

---

#### 4.4.5 规则排序

```
POST /api/v1/rules/order
```

**请求体**:

```json
{
  "rule_ids": ["rule_005", "rule_001", "rule_002", "rule_003", "rule_004"]
}
```

**响应示例**:

```json
{
  "code": 0,
  "message": "rule order updated",
  "data": null
}
```

**curl 示例**:

```bash
curl -X POST -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"rule_ids":["rule_005","rule_001","rule_002","rule_003","rule_004"]}' \
  http://127.0.0.1:9090/api/v1/rules/order
```

---

### 4.5 数据库管理

#### 4.5.1 获取数据库状态

```
GET /api/v1/geo/status
```

**响应示例**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "geosite": {
      "loaded": true,
      "version": "2026041800",
      "last_update": "2026-04-18T03:00:00Z",
      "size_bytes": 4521984,
      "categories_count": 1205,
      "update_status": "idle"
    },
    "geoip": {
      "loaded": true,
      "version": "2026041800",
      "last_update": "2026-04-18T03:00:00Z",
      "size_bytes": 3145728,
      "entries_count": 185000,
      "update_status": "idle"
    },
    "ad_filter": {
      "loaded": true,
      "last_update": "2026-04-18T03:00:00Z",
      "rule_count": 56234,
      "update_status": "idle"
    }
  }
}
```

> `update_status` 可能值: `idle`(空闲)、`downloading`(下载中)、`updating`(更新中)、`failed`(失败)

**curl 示例**:

```bash
curl -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/geo/status
```

---

#### 4.5.2 手动触发更新

```
POST /api/v1/geo/update
```

**请求体**:

```json
{
  "target": "all"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| target | string | 是 | 更新目标：`geosite` / `geoip` / `ad_filter` / `all` |

**响应示例**:

```json
{
  "code": 0,
  "message": "update started",
  "data": {
    "target": "all",
    "status": "downloading"
  }
}
```

**curl 示例**:

```bash
curl -X POST -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"target":"all"}' \
  http://127.0.0.1:9090/api/v1/geo/update
```

---

#### 4.5.3 获取数据源配置

```
GET /api/v1/geo/sources
```

**响应示例**:

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "geosite_url": "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat",
    "geoip_url": "https://github.com/Loyalsoldier/geoip/releases/latest/download/geoip.dat",
    "ad_filter_url": "https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt",
    "data_dir": "/etc/lynxdns/data",
    "auto_update": true,
    "update_cron": "0 3 * * *"
  }
}
```

**curl 示例**:

```bash
curl -H "Authorization: Bearer your-secret-key" http://127.0.0.1:9090/api/v1/geo/sources
```

---

### 4.6 WebSocket 实时数据流

#### 4.6.1 实时日志流

```
ws://<host>:<port>/api/v1/stream/logs?token=<secret>
```

**连接参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| token | string | API 认证密钥 |
| level | string | 可选，日志级别过滤：debug/info/warn/error |

**推送消息格式**:

```json
{
  "timestamp": "2026-04-19T13:00:00.123Z",
  "level": "info",
  "message": "DNS query resolved: www.baidu.com -> 110.242.68.66 via domestic"
}
```

---

#### 4.6.2 实时 DNS 查询流

```
ws://<host>:<port>/api/v1/stream/queries?token=<secret>
```

**推送消息格式**:

```json
{
  "timestamp": "2026-04-19T13:00:00.123Z",
  "domain": "www.google.com",
  "type": "A",
  "client_ip": "192.168.1.100",
  "server_used": "tls://1.1.1.1:853",
  "result_ips": ["142.250.80.46"],
  "latency_ms": 45.2,
  "action": "remote",
  "matched_rule": "geosite:geolocation-!cn",
  "cached": false
}
```

**action 字段说明**:

| 值 | 说明 |
|----|------|
| cache_hit | 缓存命中 |
| domestic | 国内 DNS 解析 |
| remote | 远程 DNS 解析 |
| blocked | 已拦截（黑名单） |
| redirected | 已重定向 |
| default | 默认 DNS 解析 |

---

## 五、数据模型定义

### 5.1 VersionInfo

```go
type VersionInfo struct {
    Version   string `json:"version"`
    BuildTime string `json:"build_time"`
    GoVersion string `json:"go_version"`
    OS        string `json:"os"`
    Arch      string `json:"arch"`
}
```

### 5.2 ServiceStatus

```go
type ServiceStatus struct {
    Running         bool                `json:"running"`
    UptimeSeconds   int64               `json:"uptime_seconds"`
    DnsQueriesTotal int64               `json:"dns_queries_total"`
    CacheHits       int64               `json:"cache_hits"`
    CacheHitRate    float64             `json:"cache_hit_rate"`
    AvgLatencyMs    float64             `json:"avg_latency_ms"`
    Goroutines      int                 `json:"goroutines"`
    MemoryMB        float64             `json:"memory_mb"`
    DnsServers      map[string][]ServerStatus `json:"dns_servers"`
}

type ServerStatus struct {
    // 基础字段（向后兼容，旧客户端可直接使用）
    Address      string  `json:"address"`
    Status       string  `json:"status"`            // up / down / unknown
    AvgLatencyMs float64 `json:"avg_latency_ms"`    // 即时探测延迟

    // 按上游维度的累计统计（新增）
    Requests         int64   `json:"requests"`          // 总请求次数
    Successes        int64   `json:"successes"`         // 成功次数
    Failures         int64   `json:"failures"`          // 失败次数（含超时）
    Timeouts         int64   `json:"timeouts"`          // 超时次数
    P95LatencyMs     float64 `json:"p95_latency_ms"`    // P95 延迟（基于最近 1024 样本）
    P99LatencyMs     float64 `json:"p99_latency_ms"`    // P99 延迟
    AvgLatencyMsHist float64 `json:"avg_latency_ms_hist"` // 基于历史样本的平均延迟

    // 熔断器状态（新增）
    CurrentState string `json:"current_state"` // closed / open / half_open
    TripCount    int64  `json:"trip_count"`    // 累计熔断次数
    RecoverCount int64  `json:"recover_count"` // 累计恢复次数

    // 协议（新增）
    Protocol string `json:"protocol"` // udp / tcp / tls / doh
}
```

### 5.3 FullConfig

```go
type FullConfig struct {
    Server   ServerConfig   `json:"server"`
    API      APIConfig      `json:"api"`
    DNS      DNSConfig      `json:"dns"`
    Advanced AdvancedConfig `json:"advanced"`
    Geo      GeoConfig      `json:"geo"`
    Routing  RoutingConfig  `json:"routing"`
    Log      LogConfig      `json:"log"`
}

type ServerConfig struct {
    Enabled    bool   `json:"enabled"`
    ListenAddr string `json:"listen_addr"`
    ListenPort int    `json:"listen_port"`
}

type APIConfig struct {
    Addr   string `json:"addr"`
    Port   int    `json:"port"`
    Secret string `json:"secret"`
}

type DNSConfig struct {
    Domestic  []string `json:"domestic"`
    Remote    []string `json:"remote"`
    Default   string   `json:"default"`
    Bootstrap []string `json:"bootstrap"`
}

type AdvancedConfig struct {
    Concurrency   int            `json:"concurrency"`
    IdleTimeout   int            `json:"idle_timeout"`
    Cache         CacheConfig    `json:"cache"`
    LeakProtection LeakConfig    `json:"leak_protection"`
}

type CacheConfig struct {
    Enabled bool `json:"enabled"`
    Size    int  `json:"size"`
    Lazy    bool `json:"lazy"`
}

type LeakConfig struct {
    Enabled bool   `json:"enabled"`
    Mode    string `json:"mode"`
}

type GeoConfig struct {
    AutoUpdate   bool   `json:"auto_update"`
    UpdateCron   string `json:"update_cron"`
    GeositeURL   string `json:"geosite_url"`
    GeoipURL     string `json:"geoip_url"`
    AdFilterURL  string `json:"ad_filter_url"`
    DataDir      string `json:"data_dir"`
}

type RoutingConfig struct {
    Geosite RoutingGroup `json:"geosite"`
    Geoip   RoutingGroup `json:"geoip"`
}

type RoutingGroup struct {
    Remote    []string `json:"remote"`
    Domestic  []string `json:"domestic"`
    AdFilter  []string `json:"ad_filter,omitempty"`
}

type LogConfig struct {
    Level string `json:"level"`
    File  string `json:"file"`
}
```

### 5.4 Rule

```go
type Rule struct {
    ID        string `json:"id"`
    Type      string `json:"type"`
    Domain    string `json:"domain"`
    Target    string `json:"target"`
    Enabled   bool   `json:"enabled"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
}
```

### 5.5 DNSStats

```go
type DNSStats struct {
    TotalQueries    int64            `json:"total_queries"`
    CacheHits       int64            `json:"cache_hits"`
    CacheHitRate    float64          `json:"cache_hit_rate"`
    AvgLatencyMs    float64          `json:"avg_latency_ms"`
    QueriesPerSecond float64         `json:"queries_per_second"`
    BlockedQueries  int64            `json:"blocked_queries"`
    RedirectedQueries int64          `json:"redirected_queries"`
    ByType          map[string]int64 `json:"by_type"`
    ByStatus        map[string]int64 `json:"by_status"`
    ByAction        map[string]int64 `json:"by_action"`
}
```

### 5.6 LookupRequest / LookupResponse

```go
type LookupRequest struct {
    Domain string `json:"domain"`
    Type   string `json:"type"`
}

type LookupResponse struct {
    Domain      string   `json:"domain"`
    Type        string   `json:"type"`
    IPs         []string `json:"ips"`
    TTL         uint32   `json:"ttl"`
    ServerUsed  string   `json:"server_used"`
    LatencyMs   float64  `json:"latency_ms"`
    Action      string   `json:"action"`
    MatchedRule string   `json:"matched_rule"`
}
```

---

## 六、错误处理

### 6.1 标准错误响应格式

```json
{
  "code": <error_code>,
  "message": "<human readable error message>",
  "data": null
}
```

### 6.2 常见错误码

| HTTP 状态码 | code | message 示例 | 触发场景 |
|-------------|------|-------------|---------|
| 200 | 0 | success | 成功 |
| 400 | 400 | invalid parameter: domain is required | 参数缺失 |
| 400 | 400 | invalid rule type: xxx | 规则类型不合法 |
| 401 | 401 | unauthorized: invalid or missing token | Token 无效 |
| 404 | 404 | rule not found: rule_xxx | 规则不存在 |
| 409 | 409 | rule already exists: .google.com | 规则重复 |
| 500 | 500 | internal server error | 内部错误 |

### 6.3 错误处理建议

前端应对每个 API 调用进行错误处理：

1. 检查 HTTP 状态码是否为 200
2. 检查 `code` 字段是否为 0
3. 非 0 时从 `message` 获取错误信息并展示给用户
4. 网络超时或连接失败时提示用户检查 LynxDNS 服务是否运行

---

## 七、LuaPP 集成指南

### 7.1 LuCI 中调用 HTTP API

在 LuCI 的 JavaScript 模块中，使用 `LuCI.request` 或 `XMLHttpRequest` 调用 API：

```javascript
// 封装 API 客户端
var LynxDNS = {
    baseUrl: 'http://127.0.0.1:9090/api/v1',
    token: '',

    request: function(method, path, data, callback) {
        var xhr = new XMLHttpRequest();
        xhr.open(method, this.baseUrl + path, true);
        xhr.setRequestHeader('Content-Type', 'application/json');
        xhr.setRequestHeader('Authorization', 'Bearer ' + this.token);
        xhr.onreadystatechange = function() {
            if (xhr.readyState === 4) {
                var resp = JSON.parse(xhr.responseText || '{}');
                callback(resp);
            }
        };
        xhr.send(data ? JSON.stringify(data) : null);
    },

    getStatus: function(cb) {
        this.request('GET', '/status', null, cb);
    },

    getConfig: function(cb) {
        this.request('GET', '/config', null, cb);
    },

    updateConfig: function(partial, cb) {
        this.request('PATCH', '/config', partial, cb);
    },

    getRules: function(cb) {
        this.request('GET', '/rules', null, cb);
    },

    addRule: function(rule, cb) {
        this.request('POST', '/rules', rule, cb);
    },

    deleteRule: function(id, cb) {
        this.request('DELETE', '/rules/' + id, null, cb);
    },

    getDnsStats: function(cb) {
        this.request('GET', '/dns/stats', null, cb);
    },

    clearCache: function(cb) {
        this.request('DELETE', '/dns/cache', null, cb);
    },

    lookup: function(domain, type, cb) {
        this.request('POST', '/dns/lookup', {domain: domain, type: type || 'A'}, cb);
    },

    getGeoStatus: function(cb) {
        this.request('GET', '/geo/status', null, cb);
    },

    triggerGeoUpdate: function(target, cb) {
        this.request('POST', '/geo/update', {target: target}, cb);
    },

    restart: function(cb) {
        this.request('POST', '/restart', null, cb);
    }
};
```

### 7.2 LuCI Lua 后端调用示例

在 LuCI 的 Lua 控制器中：

```lua
local io = require "io"
local json = require "luci.jsonc"

local API_BASE = "http://127.0.0.1:9090/api/v1"

function api_call(method, path, data, token)
    local cmd = string.format(
        'curl -s -X %s -H "Content-Type: application/json" -H "Authorization: Bearer %s" %s%s',
        method, token, API_BASE, path
    )
    if data then
        cmd = cmd .. ' -d \'' .. json.stringify(data) .. '\''
    end
    local handle = io.popen(cmd)
    local result = handle:read("*a")
    handle:close()
    return json.parse(result)
end

-- 使用示例
function get_status()
    local token = "your-secret-key"
    return api_call("GET", "/status", nil, token)
end
```

### 7.3 WebSocket 连接示例

```javascript
function connectLogStream(token, onMessage) {
    var ws = new WebSocket('ws://127.0.0.1:9090/api/v1/stream/logs?token=' + token);
    ws.onmessage = function(event) {
        var logEntry = JSON.parse(event.data);
        onMessage(logEntry);
    };
    ws.onerror = function() {
        console.error('Log WebSocket connection error');
        setTimeout(function() { connectLogStream(token, onMessage); }, 3000);
    };
    ws.onclose = function() {
        setTimeout(function() { connectLogStream(token, onMessage); }, 3000);
    };
    return ws;
}

function connectQueryStream(token, onMessage) {
    var ws = new WebSocket('ws://127.0.0.1:9090/api/v1/stream/queries?token=' + token);
    ws.onmessage = function(event) {
        var query = JSON.parse(event.data);
        onMessage(query);
    };
    ws.onerror = function() {
        console.error('Query WebSocket connection error');
        setTimeout(function() { connectQueryStream(token, onMessage); }, 3000);
    };
    ws.onclose = function() {
        setTimeout(function() { connectQueryStream(token, onMessage); }, 3000);
    };
    return ws;
}
```

### 7.4 认证配置获取

前端启动时应先从 UCI 配置获取 API Secret：

```lua
-- 在 LuCI controller 中
function get_api_config()
    local uci = require("luci.model.uci").cursor()
    return {
        addr = uci:get("lynxdns", "api", "addr") or "127.0.0.1",
        port = uci:get("lynxdns", "api", "port") or "9090",
        secret = uci:get("lynxdns", "api", "secret") or ""
    }
end
```

### 7.5 页面与 API 映射关系

| 页面 Tab | 需要调用的 API |
|----------|---------------|
| 基本设置 | GET/PATCH /api/v1/config (server 部分) |
| DNS 服务器 | GET/PATCH /api/v1/config (dns 部分) |
| 高级设置 | GET/PATCH /api/v1/config (advanced 部分) |
| GeoIP & GeoSite | GET/PATCH /api/v1/config (geo 部分) + GET /api/v1/geo/status + POST /api/v1/geo/update |
| 自定义规则 | GET/POST/PUT/DELETE /api/v1/rules |
| 日志与统计 | GET /api/v1/status + GET /api/v1/dns/stats + WebSocket /api/v1/stream/* + DELETE /api/v1/dns/cache + POST /api/v1/dns/lookup |

---

## 八、开发注意事项

1. **API Secret 安全**: 前端在浏览器端调用 API 时需注意 Token 不要暴露在页面源码中，建议通过 LuCI 后端代理转发
2. **并发安全**: PATCH 配置时，内核保证原子性，前端无需加锁
3. **WebSocket 重连**: 前端应实现自动重连机制，建议重连间隔 3-5 秒，采用指数退避策略
4. **缓存一致性**: 修改配置后（如 DNS 服务器变更），建议前端主动调用清除缓存接口
5. **长时间操作**: GeoIP/GeoSite 更新可能耗时较长，前端应使用轮询 GET /api/v1/geo/status 来跟踪进度
6. **规则 ID 规则**: 规则 ID 格式为 `rule_XXX`，由内核自动生成，前端不应自行构造

---

> **文档结束** — LynxDNS 内核 API 协议 v1.1  
> 如有疑问请联系内核开发团队
