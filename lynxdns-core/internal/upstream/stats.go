// Package upstream 提供上游 DNS 服务器的统计与熔断能力。
// 本包独立于 DNS 收发逻辑，便于后续 DoQ 等新协议复用。
package upstream

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// 延迟环形缓冲容量，采样最近 N 条样本用于分位数计算。
const latencyBufferSize = 1024

// StatsSnapshot 是统计的 JSON 友好快照，供 API 与未来 metrics 复用。
type StatsSnapshot struct {
	Requests      int64   `json:"requests"`
	Successes     int64   `json:"successes"`
	Failures      int64   `json:"failures"`
	Timeouts      int64   `json:"timeouts"`
	AvgLatencyMs  float64 `json:"avg_latency_ms_hist"`
	P95LatencyMs  float64 `json:"p95_latency_ms"`
	P99LatencyMs  float64 `json:"p99_latency_ms"`
}

// Stats 单个上游的统计结构（原子计数 + 环形延迟采样）。
//
// 字段访问约定：
//   - 计数器使用 atomic 操作，无需持锁。
//   - 延迟环形缓冲与 latSum 由 mu 保护，写入与快照均需持锁。
type Stats struct {
	requests  int64 // 总请求次数
	successes int64 // 成功次数
	failures  int64 // 失败次数（含超时）
	timeouts  int64 // 超时次数（errors.Is err DeadlineExceeded 等）

	latencies []int64 // 环形缓冲，容量 latencyBufferSize，单位 ns
	latIdx    int     // 环形写入位置
	latCount  int     // 已采样条数（用于分位数计算的有效样本数）
	latSum    int64   // 延迟总和（ns，计算平均值，O(1)）

	mu sync.Mutex
}

// NewStats 创建统计实例。
func NewStats() *Stats {
	return &Stats{
		latencies: make([]int64, latencyBufferSize),
	}
}

// RecordSuccess 记录一次成功请求的延迟。
// 不返回 error，内部 recover 兜底，绝不影响 DNS 请求路径。
func (s *Stats) RecordSuccess(latency time.Duration) {
	defer func() { _ = recover() }()
	if s == nil {
		return
	}

	atomic.AddInt64(&s.requests, 1)
	atomic.AddInt64(&s.successes, 1)

	ns := latency.Nanoseconds()
	if ns < 0 {
		ns = 0
	}

	s.mu.Lock()
	s.latencies[s.latIdx] = ns
	s.latIdx = (s.latIdx + 1) % latencyBufferSize
	if s.latCount < latencyBufferSize {
		s.latCount++
	}
	s.latSum += ns
	s.mu.Unlock()
}

// RecordFailure 记录一次失败请求。isTimeout 为 true 时同时累加超时计数。
// 不返回 error，内部 recover 兜底，绝不影响 DNS 请求路径。
func (s *Stats) RecordFailure(isTimeout bool) {
	defer func() { _ = recover() }()
	if s == nil {
		return
	}

	atomic.AddInt64(&s.requests, 1)
	atomic.AddInt64(&s.failures, 1)
	if isTimeout {
		atomic.AddInt64(&s.timeouts, 1)
	}
}

// Snapshot 返回当前统计快照。计算 P95/P99 时复制延迟切片并排序。
func (s *Stats) Snapshot() StatsSnapshot {
	if s == nil {
		return StatsSnapshot{}
	}

	snap := StatsSnapshot{
		Requests:  atomic.LoadInt64(&s.requests),
		Successes: atomic.LoadInt64(&s.successes),
		Failures:  atomic.LoadInt64(&s.failures),
		Timeouts:  atomic.LoadInt64(&s.timeouts),
	}

	s.mu.Lock()
	count := s.latCount
	sum := s.latSum
	if count > 0 {
		buf := make([]int64, count)
		if count <= latencyBufferSize {
			// 取前 count 条（环形写入顺序不影响分位数计算，复制即可）
			copy(buf, s.latencies[:count])
		} else {
			copy(buf, s.latencies)
		}
		snap.AvgLatencyMs = float64(sum) / float64(count) / 1e6
		snap.P95LatencyMs = percentileFromSorted(buf, 0.95)
		snap.P99LatencyMs = percentileFromSorted(buf, 0.99)
	}
	s.mu.Unlock()

	return snap
}

// percentileFromSorted 对已采样的延迟切片排序后计算分位数，返回 ms。
// 使用 "nearest rank" 方法，与多数监控工具一致。
func percentileFromSorted(values []int64, p float64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })

	// nearest rank: rank = ceil(p * n)
	rank := int(math.Ceil(p * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return float64(values[rank-1]) / 1e6
}
