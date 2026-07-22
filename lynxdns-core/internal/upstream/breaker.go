package upstream

import (
	"sync"
	"sync/atomic"
	"time"
)

// 熔断器状态常量。
const (
	StateClosed   int32 = 0 // 正常放行
	StateOpen     int32 = 1 // 熔断中，拒绝请求
	StateHalfOpen int32 = 2 // 半开，放行一次探针请求
)

// 熔断器配置常量（初版硬编码，后续可配置化）。
const (
	failThreshold = 5              // 连续失败多少次触发熔断
	cooldown      = 10 * time.Second // 熔断后冷却多久进入 half-open
)

// 字符串状态映射，用于 Snapshot 输出。
var stateNames = map[int32]string{
	StateClosed:   "closed",
	StateOpen:     "open",
	StateHalfOpen: "half_open",
}

// BreakerSnapshot 是熔断器的 JSON 友好快照。
type BreakerSnapshot struct {
	CurrentState string `json:"current_state"` // closed/open/half_open
	TripCount    int64  `json:"trip_count"`
	RecoverCount int64  `json:"recover_count"`
}

// Breaker 单个上游的熔断器。
//
// 状态迁移：
//   - closed  --连续失败达阈值--> open
//   - open    --冷却时间到--> half-open（由 AllowRequest 触发迁移）
//   - half-open --探针成功--> closed（恢复）
//   - half-open --探针失败--> open
type Breaker struct {
	state            int32 // 当前状态，atomic 读写
	consecutiveFails int64 // 连续失败数，atomic 读写
	trips            int64 // 累计熔断次数
	recovers         int64 // 累计恢复次数
	openedAt         int64 // 熔断打开时间 ns（UnixNano），0 表示未熔断

	mu sync.Mutex // 保护状态迁移的临界区
}

// NewBreaker 创建熔断器实例，初始状态 closed。
func NewBreaker() *Breaker {
	return &Breaker{
		state: StateClosed,
	}
}

// AllowRequest 判断是否放行请求。
//
// - closed：放行。
// - open：若冷却时间已过，迁移到 half-open 并放行（本次为探针）；
//   否则拒绝。
// - half-open：放行（探针请求）。
//
// 不返回 error，内部 recover 兜底。
func (b *Breaker) AllowRequest() bool {
	defer func() { _ = recover() }()
	if b == nil {
		return true
	}

	state := atomic.LoadInt32(&b.state)
	switch state {
	case StateClosed:
		return true
	case StateHalfOpen:
		return true
	case StateOpen:
		// 检查冷却时间，可能迁移到 half-open
		b.mu.Lock()
		defer b.mu.Unlock()

		// 双重检查，防止并发竞争
		if atomic.LoadInt32(&b.state) != StateOpen {
			return true
		}

		openedAt := atomic.LoadInt64(&b.openedAt)
		if time.Now().UnixNano()-openedAt >= int64(cooldown) {
			atomic.StoreInt32(&b.state, StateHalfOpen)
			return true
		}
		return false
	default:
		return true
	}
}

// RecordSuccess 记录一次成功请求，可能触发 half-open -> closed 恢复。
func (b *Breaker) RecordSuccess() {
	defer func() { _ = recover() }()
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	state := atomic.LoadInt32(&b.state)
	if state == StateHalfOpen {
		// 探针成功，恢复
		atomic.StoreInt32(&b.state, StateClosed)
		atomic.AddInt64(&b.recovers, 1)
	}
	atomic.StoreInt64(&b.consecutiveFails, 0)
}

// RecordFailure 记录一次失败请求，可能触发 closed -> open 熔断。
func (b *Breaker) RecordFailure() {
	defer func() { _ = recover() }()
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	state := atomic.LoadInt32(&b.state)
	if state == StateHalfOpen {
		// 探针失败，重新熔断
		b.trip()
		return
	}

	consecutive := atomic.AddInt64(&b.consecutiveFails, 1)
	if state == StateClosed && consecutive >= int64(failThreshold) {
		b.trip()
	}
}

// trip 执行熔断迁移（调用方需持有 b.mu）。
func (b *Breaker) trip() {
	atomic.StoreInt32(&b.state, StateOpen)
	atomic.AddInt64(&b.trips, 1)
	atomic.StoreInt64(&b.openedAt, time.Now().UnixNano())
}

// Snapshot 返回熔断器当前快照。
func (b *Breaker) Snapshot() BreakerSnapshot {
	if b == nil {
		return BreakerSnapshot{CurrentState: stateNames[StateClosed]}
	}

	state := atomic.LoadInt32(&b.state)
	name, ok := stateNames[state]
	if !ok {
		name = stateNames[StateClosed]
	}

	return BreakerSnapshot{
		CurrentState: name,
		TripCount:    atomic.LoadInt64(&b.trips),
		RecoverCount: atomic.LoadInt64(&b.recovers),
	}
}
