package api

import (
	"claude-api/internal/database"
	"claude-api/internal/logger"
	"claude-api/internal/models"
	"context"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// AccountPool 账号池，用于缓存启用的账号列表
// 避免每次请求都查询数据库，显著提升高并发性能
// @author ygw
type AccountPool struct {
	accounts          []*models.Account // 缓存的账号列表
	mu                sync.RWMutex      // 读写锁
	lastRefresh       time.Time         // 上次刷新时间
	refreshInterval   time.Duration     // 刷新间隔
	db                *database.DB      // 数据库连接
	cfg               accountPoolConfig // 配置
	refreshing        atomic.Bool       // 是否正在刷新
	roundRobinIndex   uint32            // 轮询索引（用于 round_robin 模式）
	lastUsedTime      sync.Map          // 账号最后使用时间 map[string]time.Time（用于 cooldown 模式，按选号时刻计冷却）
	cooldownReleaseAt sync.Map          // 账号请求完成后强制冷却到的时间 map[string]time.Time（用于 cooldown 模式，与 lastUsedTime 取 max）
	cooldownInFlight  sync.Map          // 账号当前正在处理请求的开始时间 map[string]time.Time（用于 cooldown 模式，in-flight 期间不可被再次选中）
	rpmStates         sync.Map          // 账号 RPM 状态 map[string]*rpmAccountState（用于 rpm 模式）
}

// RPM 模式相关常量
const (
	// rpmAccountReleaseCooldown 请求成功结束后账号需冷却的时长
	rpmAccountReleaseCooldown = 5 * time.Second
	// rpmAccountInFlightTimeout in-flight 状态的兜底超时，超过则强制释放（防止泄漏）
	rpmAccountInFlightTimeout = 3 * time.Minute
	// cooldownPostRequestForced cooldown 模式下请求完成后强制额外冷却时长
	// 与 cooldownSeconds 取 max，避免「长请求结束后立刻被再次选中」
	cooldownPostRequestForced = 5 * time.Second
	// cooldownInFlightTimeout cooldown 模式 in-flight 状态的兜底超时，超过则强制释放（防止状态泄漏）
	cooldownInFlightTimeout = 2*time.Minute + 6*time.Second
	// cooldownUrgentExpiryWindow cooldown 模式下「即将过期」账号的优先窗口
	// 剩余有效期 ≤ 该窗口的账号优先被选中（与前端 isAccountExpiringSoon 的 7 天阈值保持一致）
	cooldownUrgentExpiryWindow = 7 * 24 * time.Hour
)

// rpmAccountState 单个账号的 RPM 调度状态
type rpmAccountState struct {
	mu         sync.Mutex
	timestamps []time.Time // 60 秒滑动窗口内的选号时间戳
	inFlight   bool        // 当前是否有正在处理的请求
	claimedAt  time.Time   // 进入 in-flight 的时间（用于兜底超时清理）
	releaseAt  time.Time   // 最早可被再次选中的时间（请求结束后 + 冷却时长）
}

// accountPoolConfig 账号池配置
type accountPoolConfig struct {
	lazyEnabled            bool   // 是否启用懒加载模式
	lazyPoolSize           int    // 懒加载池大小
	lazyOrderBy            string // 懒加载排序字段
	lazyOrderDesc          bool   // 懒加载是否降序
	selectionMode          string // 账号选择方式: sequential, random, weighted_random, round_robin, cooldown, rpm
	cooldownSeconds        int    // 冷却时间（秒），cooldown 模式下生效
	rpmLimit               int    // 60 秒滑动窗口内最多被调度次数（含失败），rpm 模式下生效
	rpmFailureCooldownSecs int    // 失败后账号冷却时长（秒），rpm 模式下生效
}

// NewAccountPool 创建新的账号池
func NewAccountPool(db *database.DB, refreshInterval time.Duration) *AccountPool {
	if refreshInterval <= 0 {
		refreshInterval = 10 * time.Second // 默认 10 秒刷新
	}
	return &AccountPool{
		accounts:        make([]*models.Account, 0),
		refreshInterval: refreshInterval,
		db:              db,
	}
}

// SetConfig 设置配置
func (p *AccountPool) SetConfig(lazyEnabled bool, lazyPoolSize int, lazyOrderBy string, lazyOrderDesc bool, selectionMode string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// 验证选择模式，无效则使用默认值
	if selectionMode == "" {
		selectionMode = models.AccountSelectionSequential
	}
	p.cfg = accountPoolConfig{
		lazyEnabled:   lazyEnabled,
		lazyPoolSize:  lazyPoolSize,
		lazyOrderBy:   lazyOrderBy,
		lazyOrderDesc: lazyOrderDesc,
		selectionMode: selectionMode,
	}
}

// Start 启动后台刷新任务
func (p *AccountPool) Start(ctx context.Context) {
	// 立即刷新一次
	p.Refresh(ctx)

	go func() {
		ticker := time.NewTicker(p.refreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.Refresh(ctx)
			}
		}
	}()
}

// Refresh 刷新账号缓存
func (p *AccountPool) Refresh(ctx context.Context) {
	startTime := time.Now()

	// 防止并发刷新
	if !p.refreshing.CompareAndSwap(false, true) {
		return
	}
	defer p.refreshing.Store(false)

	// 从数据库配置更新最新配置（支持动态更新）
	dbCfg := p.db.GetConfig()
	p.mu.Lock()
	p.cfg.selectionMode = dbCfg.AccountSelectionMode
	if p.cfg.selectionMode == "" {
		p.cfg.selectionMode = models.AccountSelectionSequential
	}
	p.cfg.cooldownSeconds = dbCfg.AccountCooldownSeconds
	if p.cfg.cooldownSeconds <= 0 {
		p.cfg.cooldownSeconds = models.DefaultAccountCooldownSeconds
	}
	p.cfg.rpmLimit = dbCfg.AccountRPMLimit
	if p.cfg.rpmLimit <= 0 {
		p.cfg.rpmLimit = models.DefaultAccountRPMLimit
	}
	p.cfg.rpmFailureCooldownSecs = dbCfg.AccountRPMFailureCooldownSeconds
	if p.cfg.rpmFailureCooldownSecs < 0 {
		p.cfg.rpmFailureCooldownSecs = models.DefaultAccountRPMFailureCooldownSeconds
	}
	p.cfg.lazyEnabled = dbCfg.LazyAccountPoolEnabled
	p.cfg.lazyPoolSize = dbCfg.LazyAccountPoolSize
	p.cfg.lazyOrderBy = dbCfg.LazyAccountPoolOrderBy
	p.cfg.lazyOrderDesc = dbCfg.LazyAccountPoolOrderDesc
	cfg := p.cfg
	p.mu.Unlock()

	var accounts []*models.Account
	var err error

	// 只获取状态为 normal 的账号（优化：封控/用尽的账号不再进入池）
	if cfg.lazyEnabled {
		accounts, err = p.db.ListAccountsByStatus(ctx, models.AccountStatusNormal, cfg.lazyOrderBy, cfg.lazyOrderDesc)
		if err == nil && cfg.lazyPoolSize > 0 && len(accounts) > cfg.lazyPoolSize {
			accounts = accounts[:cfg.lazyPoolSize]
		}
	} else {
		accounts, err = p.db.ListAccountsByStatus(ctx, models.AccountStatusNormal, "created_at", true)
	}

	if err != nil {
		elapsed := time.Since(startTime)
		logger.Error("[账号池] 刷新失败 - 耗时: %.0fms, 错误: %v", elapsed.Seconds()*1000, err)
		return
	}

	// 按有效日期排序：有效日期较近的账号优先使用，无有效日期的排后面
	sort.SliceStable(accounts, func(i, j int) bool {
		iExpiry := accounts[i].TokenExpiry
		jExpiry := accounts[j].TokenExpiry

		iHas := iExpiry != nil && *iExpiry > 0
		jHas := jExpiry != nil && *jExpiry > 0

		if !iHas && !jHas {
			return false
		}
		if !iHas {
			return false
		}
		if !jHas {
			return true
		}
		return *iExpiry < *jExpiry
	})

	p.mu.Lock()
	p.accounts = accounts
	p.lastRefresh = time.Now()
	p.mu.Unlock()

	// 清理已离池账号在 rpmStates 中的残留状态（避免长期内存泄漏 + 防止账号同 ID 重建时继承老冷却）
	activeIDs := make(map[string]struct{}, len(accounts))
	for _, a := range accounts {
		activeIDs[a.ID] = struct{}{}
	}
	if removed := p.pruneRPMStates(activeIDs); removed > 0 {
		logger.Debug("[账号池] RPM 状态清理: 移除 %d 个已离池账号的状态", removed)
	}
	if removed := p.pruneCooldownStates(activeIDs); removed > 0 {
		logger.Debug("[账号池] cooldown 状态清理: 移除 %d 个已离池账号的状态", removed)
	}

	elapsed := time.Since(startTime)
	logger.Debug("[账号池] 刷新完成 - 账号数: %d, 选择方式: %s, 耗时: %.0fms", len(accounts), cfg.selectionMode, elapsed.Seconds()*1000)
}

// pruneCooldownStates 清理已不在账号池中的账号的 cooldown 状态
// lastUsedTime / cooldownReleaseAt：跳过 in-flight 中的账号（让 ReleaseCooldown 自然清理，下次 Refresh 收敛）
// cooldownInFlight：超过 3min 兜底超时视为状态泄漏，强制清理；未超时的等请求自然结束
// 返回被清理的条目数
func (p *AccountPool) pruneCooldownStates(activeIDs map[string]struct{}) int {
	removed := 0

	cleanIfOrphan := func(m *sync.Map) {
		m.Range(func(key, value any) bool {
			id, ok := key.(string)
			if !ok {
				return true
			}
			if _, alive := activeIDs[id]; alive {
				return true
			}
			// 仍在 in-flight 中：让请求结束后 ReleaseCooldown 自然清理
			if _, inFlight := p.cooldownInFlight.Load(id); inFlight {
				return true
			}
			m.Delete(key)
			removed++
			return true
		})
	}
	cleanIfOrphan(&p.lastUsedTime)
	cleanIfOrphan(&p.cooldownReleaseAt)

	// cooldownInFlight 自身的清理：离池且超过 3min 兜底超时的强制释放
	now := time.Now()
	p.cooldownInFlight.Range(func(key, value any) bool {
		id, ok := key.(string)
		if !ok {
			return true
		}
		if _, alive := activeIDs[id]; alive {
			return true
		}
		claimedAt, ok := value.(time.Time)
		if !ok {
			return true
		}
		if now.Sub(claimedAt) <= cooldownInFlightTimeout {
			return true
		}
		p.cooldownInFlight.Delete(key)
		removed++
		return true
	})

	return removed
}

// pruneRPMStates 清理已不在账号池中的账号的 RPM 状态
// 跳过 in-flight 状态的账号（仍在执行的请求结束后由下次 Refresh 收敛）
// 返回被清理的条目数
func (p *AccountPool) pruneRPMStates(activeIDs map[string]struct{}) int {
	removed := 0
	p.rpmStates.Range(func(key, value any) bool {
		id, ok := key.(string)
		if !ok {
			return true
		}
		if _, alive := activeIDs[id]; alive {
			return true
		}
		st := value.(*rpmAccountState)
		st.mu.Lock()
		inFlight := st.inFlight
		st.mu.Unlock()
		if inFlight {
			return true
		}
		p.rpmStates.Delete(key)
		removed++
		return true
	})
	return removed
}

// GetAccount 获取一个账号（根据配置选择方式）
func (p *AccountPool) GetAccount() *models.Account {
	p.mu.RLock()
	accounts := p.accounts
	mode := p.cfg.selectionMode
	p.mu.RUnlock()

	if len(accounts) == 0 {
		return nil
	}

	return p.selectAccount(accounts, mode)
}

// selectAccount 根据选择模式选择账号
func (p *AccountPool) selectAccount(accounts []*models.Account, mode string) *models.Account {
	if len(accounts) == 0 {
		return nil
	}

	switch mode {
	case models.AccountSelectionRandom:
		// 随机选择
		return accounts[rand.Intn(len(accounts))]

	case models.AccountSelectionWeightedRandom:
		// 加权随机选择
		return p.selectWeightedRandom(accounts)

	case models.AccountSelectionRoundRobin:
		// 轮询选择
		idx := atomic.AddUint32(&p.roundRobinIndex, 1) - 1
		return accounts[idx%uint32(len(accounts))]

	case models.AccountSelectionCooldown:
		// 冷却时间选择
		return p.selectCooldown(accounts)

	case models.AccountSelectionRPM:
		// RPM 限制选择
		return p.selectRPM(accounts)

	default: // sequential 或其他
		// 顺序选择（返回第一个）
		return accounts[0]
	}
}

// selectWeightedRandom 加权随机选择账号
// 权重计算因素：
// 1. 配额剩余比例（权重最高）
// 2. 最近使用时间（越久未使用权重越高）
// 3. 成功率（成功次数越多权重越高）
func (p *AccountPool) selectWeightedRandom(accounts []*models.Account) *models.Account {
	if len(accounts) == 0 {
		return nil
	}
	if len(accounts) == 1 {
		return accounts[0]
	}

	type weightedAccount struct {
		account *models.Account
		weight  float64
	}

	weightedAccounts := make([]weightedAccount, 0, len(accounts))
	now := time.Now()

	for _, acc := range accounts {
		weight := 1.0

		// 1. 配额剩余权重（配额剩余越多，权重越高）
		if acc.UsageLimit > 0 {
			remaining := acc.UsageLimit - acc.UsageCurrent
			if remaining < 0 {
				remaining = 0
			}
			quotaRatio := remaining / acc.UsageLimit
			// 配额剩余比例 * 10 作为权重（最高加10分）
			weight += quotaRatio * 10
		} else {
			// 没有配额信息的账号给予中等权重
			weight += 5
		}

		// 2. 最近使用时间权重（越久未使用，权重越高）
		if acc.LastRefreshTime != nil && *acc.LastRefreshTime != "" {
			lastTime, err := time.Parse(models.TimeFormat, *acc.LastRefreshTime)
			if err == nil {
				hoursSinceUse := now.Sub(lastTime).Hours()
				// 每小时未使用增加 0.5 权重，最高加 5 分
				timeWeight := hoursSinceUse * 0.5
				if timeWeight > 5 {
					timeWeight = 5
				}
				weight += timeWeight
			}
		} else {
			// 从未使用过的账号给予较高权重
			weight += 3
		}

		// 3. 成功率权重（成功次数越多，说明账号越稳定）
		totalRequests := acc.SuccessCount + acc.ErrorCount
		if totalRequests > 0 {
			successRate := float64(acc.SuccessCount) / float64(totalRequests)
			// 成功率 * 3 作为权重（最高加3分）
			weight += successRate * 3
		} else {
			// 新账号给予中等权重
			weight += 1.5
		}

		weightedAccounts = append(weightedAccounts, weightedAccount{
			account: acc,
			weight:  weight,
		})
	}

	// 计算总权重
	totalWeight := 0.0
	for _, wa := range weightedAccounts {
		totalWeight += wa.weight
	}

	if totalWeight == 0 {
		return accounts[0]
	}

	// 加权随机选择
	r := rand.Float64() * totalWeight
	for _, wa := range weightedAccounts {
		r -= wa.weight
		if r <= 0 {
			return wa.account
		}
	}

	return accounts[0]
}

// selectCooldown 冷却时间选择账号
// 过滤掉处于冷却期或 in-flight 中的账号，从可用账号中按轮询方式选择
// 优先选中剩余有效期 ≤ cooldownUrgentExpiryWindow 的账号（避免临期账号过期作废）
// 所有账号都不可用时返回 nil，由上层返回错误提示
// 通过 LoadOrStore 原子地占用 in-flight 标记，防止并发请求选中同一账号
func (p *AccountPool) selectCooldown(accounts []*models.Account) *models.Account {
	p.mu.RLock()
	cooldownDuration := time.Duration(p.cfg.cooldownSeconds) * time.Second
	p.mu.RUnlock()

	now := time.Now()
	urgentCutoff := now.Add(cooldownUrgentExpiryWindow).Unix()
	nowUnix := now.Unix()

	var urgent, normal []*models.Account

	for _, acc := range accounts {
		// in-flight 检查：账号正在处理请求时不可被再次选中
		// 兜底：超过最大持续时间视为状态泄漏，强制释放
		if claimedAtV, ok := p.cooldownInFlight.Load(acc.ID); ok {
			if now.Sub(claimedAtV.(time.Time)) > cooldownInFlightTimeout {
				logger.Warn("[账号池] cooldown 模式: 账号 %s in-flight 持续 %s 已超限，强制释放", acc.ID, cooldownInFlightTimeout)
				p.cooldownInFlight.Delete(acc.ID)
			} else {
				continue // 仍在处理中，跳过
			}
		}
		if lastUsed, ok := p.lastUsedTime.Load(acc.ID); ok {
			if now.Sub(lastUsed.(time.Time)) < cooldownDuration {
				continue // 仍在选号时刻冷却中，跳过
			}
		}
		// 请求完成后的强制冷却：兜底「请求耗时已超过 cooldownSeconds」时账号能立即被再次选中
		if relAt, ok := p.cooldownReleaseAt.Load(acc.ID); ok {
			if now.Before(relAt.(time.Time)) {
				continue
			}
		}
		// 分组：剩余有效期 ≤ 7 天且未过期的账号进入紧急组优先消耗
		// 已过期 / 长期 / 无 expiry 的账号统一走普通组兜底
		if acc.TokenExpiry != nil && *acc.TokenExpiry > nowUnix && *acc.TokenExpiry <= urgentCutoff {
			urgent = append(urgent, acc)
		} else {
			normal = append(normal, acc)
		}
	}

	if len(urgent)+len(normal) == 0 {
		logger.Debug("[账号池] 冷却模式: 所有 %d 个账号均在冷却期内或处理中（%ds），拒绝调度", len(accounts), p.cfg.cooldownSeconds)
		return nil
	}

	// 一次性推进 RR 计数器并在两组间共享起点，避免 fallback 时计数器被推进两次
	// 每组各自对其长度取模，互不影响
	start := atomic.AddUint32(&p.roundRobinIndex, 1) - 1
	if picked := p.tryClaimCooldownRR(urgent, start); picked != nil {
		return picked
	}
	if picked := p.tryClaimCooldownRR(normal, start); picked != nil {
		return picked
	}

	logger.Debug("[账号池] 冷却模式: 可用 %d 个账号（紧急 %d / 普通 %d）均被并发抢占，拒绝调度", len(urgent)+len(normal), len(urgent), len(normal))
	return nil
}

// tryClaimCooldownRR 在给定可用集合内按 round-robin 顺序尝试占用 in-flight
// start 由调用方传入并跨组共享，确保单次 selectCooldown 调用只推进 RR 计数器一次
// 占位成功返回该账号；全部被并发抢占返回 nil（由调用方决定回退到下一组或拒绝）
func (p *AccountPool) tryClaimCooldownRR(available []*models.Account, start uint32) *models.Account {
	n := uint32(len(available))
	if n == 0 {
		return nil
	}
	for i := uint32(0); i < n; i++ {
		candidate := available[(start+i)%n]
		claimedAt := time.Now()
		if _, loaded := p.cooldownInFlight.LoadOrStore(candidate.ID, claimedAt); loaded {
			continue
		}
		p.lastUsedTime.Store(candidate.ID, claimedAt)
		return candidate
	}
	return nil
}

// NotifyUsed 通知账号已被使用（记录使用时间，用于 cooldown 模式）
func (p *AccountPool) NotifyUsed(accountID string) {
	p.lastUsedTime.Store(accountID, time.Now())
}

// selectRPM RPM 限制选择账号
// 规则：
//  1. 单账号同时只能有一个 in-flight 请求
//  2. 请求成功结束 → 5s 冷却；请求失败 → 配置的失败冷却时长（默认 90s）
//  3. 60 秒滑动窗口内的选号次数达到 rpmLimit 后该账号也不可被选
//
// 选号成功时立即在窗口中记录时间戳并占用 in-flight；调用方在请求结束时
// 必须调用 ReleaseRPM 释放占用并启动冷却（成功传 failed=false，失败传 failed=true）。
func (p *AccountPool) selectRPM(accounts []*models.Account) *models.Account {
	p.mu.RLock()
	limit := p.cfg.rpmLimit
	p.mu.RUnlock()
	if limit <= 0 {
		limit = models.DefaultAccountRPMLimit
	}

	n := uint32(len(accounts))
	if n == 0 {
		return nil
	}
	start := atomic.AddUint32(&p.roundRobinIndex, 1) - 1
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	for i := uint32(0); i < n; i++ {
		acc := accounts[(start+i)%n]
		v, _ := p.rpmStates.LoadOrStore(acc.ID, &rpmAccountState{})
		st := v.(*rpmAccountState)

		st.mu.Lock()
		// in-flight 兜底：超过最大持续时间视为状态泄漏，强制释放
		if st.inFlight && now.Sub(st.claimedAt) > rpmAccountInFlightTimeout {
			logger.Warn("[账号池] RPM 模式: 账号 %s in-flight 持续 %s 已超限，强制释放", acc.ID, rpmAccountInFlightTimeout)
			st.inFlight = false
			st.releaseAt = now
		}
		if st.inFlight {
			st.mu.Unlock()
			continue
		}
		if now.Before(st.releaseAt) {
			st.mu.Unlock()
			continue
		}
		// 清理 60 秒外的过期时间戳
		kept := st.timestamps[:0]
		for _, t := range st.timestamps {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		st.timestamps = kept
		if len(st.timestamps) >= limit {
			st.mu.Unlock()
			continue
		}
		// 占用账号
		st.inFlight = true
		st.claimedAt = now
		st.timestamps = append(st.timestamps, now)
		st.mu.Unlock()
		return acc
	}

	logger.Debug("[账号池] RPM 模式: 所有 %d 个账号均不可调度（in-flight/冷却中/达上限 %d），拒绝调度", n, limit)
	return nil
}

// RPMSnapshot 单个账号的 RPM 状态快照（用于账号列表展示）
type RPMSnapshot struct {
	InFlight          bool  `json:"inFlight"`          // 当前是否有正在处理的请求
	CooldownRemaining int   `json:"cooldownRemaining"` // 剩余冷却秒数
	RecentCount       int   `json:"recentCount"`       // 60s 滑动窗口内已用次数
	ReleaseAt         int64 `json:"releaseAt"`         // 最早可被选中的 unix 秒，前端可自行倒计时
}

// GetRPMSnapshot 非破坏性读取账号的 RPM 状态快照
// 锁内只复制几个字段（~100ns 级），不会争用热路径
// 不修改 timestamps（不在读路径里 prune）
func (p *AccountPool) GetRPMSnapshot(accountID string) (RPMSnapshot, bool) {
	v, ok := p.rpmStates.Load(accountID)
	if !ok {
		return RPMSnapshot{}, false
	}
	st := v.(*rpmAccountState)
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	st.mu.Lock()
	snap := RPMSnapshot{
		InFlight:  st.inFlight,
		ReleaseAt: 0,
	}
	if !st.releaseAt.IsZero() {
		snap.ReleaseAt = st.releaseAt.Unix()
	}
	for _, t := range st.timestamps {
		if t.After(cutoff) {
			snap.RecentCount++
		}
	}
	releaseAt := st.releaseAt
	st.mu.Unlock()

	if releaseAt.After(now) {
		snap.CooldownRemaining = int(releaseAt.Sub(now).Seconds())
		if snap.CooldownRemaining < 1 {
			snap.CooldownRemaining = 1 // 不到 1s 也展示 1，避免显示 0 引起歧义
		}
	}
	return snap, true
}

// ClearAllRPMCooldowns 立即解除所有账号的 RPM 冷却与 60s 窗口计数
// 不会动 inFlight（避免破坏正在处理的请求；in-flight 兜底超时仍生效）
// 返回受影响的账号数（即 rpmStates 中存在记录的账号数）
func (p *AccountPool) ClearAllRPMCooldowns() int {
	count := 0
	p.rpmStates.Range(func(key, value any) bool {
		st := value.(*rpmAccountState)
		st.mu.Lock()
		st.releaseAt = time.Time{}
		st.timestamps = st.timestamps[:0]
		st.mu.Unlock()
		count++
		return true
	})
	return count
}

// ReleaseRPM 释放账号的 in-flight 状态并启动冷却
// failed=false → 固定 5s 成功冷却
// failed=true → 失败冷却时长由配置 AccountRPMFailureCooldownSeconds 决定（默认 90s，可配置）
// 多次调用幂等且不会"降级"冷却：releaseAt 仅在新值更晚时才更新，确保 defer 兜底不会
// 把已设的失败长冷却覆盖为短冷却。对未占用过的账号无副作用
func (p *AccountPool) ReleaseRPM(accountID string, failed bool) {
	v, ok := p.rpmStates.Load(accountID)
	if !ok {
		return
	}
	cooldown := rpmAccountReleaseCooldown
	if failed {
		p.mu.RLock()
		secs := p.cfg.rpmFailureCooldownSecs
		p.mu.RUnlock()
		if secs < 0 {
			secs = models.DefaultAccountRPMFailureCooldownSeconds
		}
		cooldown = time.Duration(secs) * time.Second
	}
	newReleaseAt := time.Now().Add(cooldown)
	st := v.(*rpmAccountState)
	st.mu.Lock()
	st.inFlight = false
	if newReleaseAt.After(st.releaseAt) {
		st.releaseAt = newReleaseAt
	}
	st.mu.Unlock()
}

// ReleaseCooldown 标记账号请求已完成，清除 in-flight 标记并启动「完成后强制 cooldownPostRequestForced」冷却
// in-flight 清理无条件执行，避免请求中途模式切换导致 in-flight 泄漏直到 3min 兜底超时
// 5s 兜底冷却仅在当前仍为 cooldown 模式时设置；多次调用幂等且不会降级（取 max）
// 与 selectCooldown 的 lastUsedTime+cooldownSeconds 一起取 max 决定下次可选时间
func (p *AccountPool) ReleaseCooldown(accountID string) {
	// 无条件清除 in-flight 占用：即使请求期间模式从 cooldown 切走，也要回收占用
	// 其它模式不使用 cooldownInFlight，对它们而言 Delete 是无副作用的 no-op
	p.cooldownInFlight.Delete(accountID)

	p.mu.RLock()
	mode := p.cfg.selectionMode
	p.mu.RUnlock()
	if mode != models.AccountSelectionCooldown {
		return
	}
	newReleaseAt := time.Now().Add(cooldownPostRequestForced)
	if existing, ok := p.cooldownReleaseAt.Load(accountID); ok {
		if !newReleaseAt.After(existing.(time.Time)) {
			return
		}
	}
	p.cooldownReleaseAt.Store(accountID, newReleaseAt)
}

// GetAccountExcluding 获取一个账号，排除指定的账号 ID
func (p *AccountPool) GetAccountExcluding(excludeIDs []string) *models.Account {
	p.mu.RLock()
	accounts := p.accounts
	mode := p.cfg.selectionMode
	p.mu.RUnlock()

	if len(accounts) == 0 {
		return nil
	}

	// 构建排除集合
	excludeSet := make(map[string]bool)
	for _, id := range excludeIDs {
		excludeSet[id] = true
	}

	// 过滤可用账号
	var available []*models.Account
	for _, acc := range accounts {
		if !excludeSet[acc.ID] {
			available = append(available, acc)
		}
	}

	if len(available) == 0 {
		return nil
	}

	return p.selectAccount(available, mode)
}

// GetSelectionMode 获取当前选择模式（用于日志等）
func (p *AccountPool) GetSelectionMode() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cfg.selectionMode
}

// SortAccountsByWeight 按权重排序账号（用于调试和展示）
func (p *AccountPool) SortAccountsByWeight(accounts []*models.Account) []*models.Account {
	if len(accounts) <= 1 {
		return accounts
	}

	type weightedAccount struct {
		account *models.Account
		weight  float64
	}

	now := time.Now()
	weightedAccounts := make([]weightedAccount, 0, len(accounts))

	for _, acc := range accounts {
		weight := 1.0

		// 配额剩余权重
		if acc.UsageLimit > 0 {
			remaining := acc.UsageLimit - acc.UsageCurrent
			if remaining < 0 {
				remaining = 0
			}
			weight += (remaining / acc.UsageLimit) * 10
		} else {
			weight += 5
		}

		// 最近使用时间权重
		if acc.LastRefreshTime != nil && *acc.LastRefreshTime != "" {
			lastTime, err := time.Parse(models.TimeFormat, *acc.LastRefreshTime)
			if err == nil {
				hoursSinceUse := now.Sub(lastTime).Hours()
				timeWeight := hoursSinceUse * 0.5
				if timeWeight > 5 {
					timeWeight = 5
				}
				weight += timeWeight
			}
		} else {
			weight += 3
		}

		// 成功率权重
		totalRequests := acc.SuccessCount + acc.ErrorCount
		if totalRequests > 0 {
			successRate := float64(acc.SuccessCount) / float64(totalRequests)
			weight += successRate * 3
		} else {
			weight += 1.5
		}

		weightedAccounts = append(weightedAccounts, weightedAccount{
			account: acc,
			weight:  weight,
		})
	}

	// 按权重降序排序
	sort.Slice(weightedAccounts, func(i, j int) bool {
		return weightedAccounts[i].weight > weightedAccounts[j].weight
	})

	result := make([]*models.Account, len(weightedAccounts))
	for i, wa := range weightedAccounts {
		result[i] = wa.account
	}
	return result
}

// Count 返回缓存的账号数量
func (p *AccountPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.accounts)
}

// GetAll 获取所有缓存的账号（用于兼容需要完整列表的场景）
func (p *AccountPool) GetAll() []*models.Account {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// 返回副本，避免外部修改
	result := make([]*models.Account, len(p.accounts))
	copy(result, p.accounts)
	return result
}

// IsStale 检查缓存是否过期
func (p *AccountPool) IsStale() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return time.Since(p.lastRefresh) > p.refreshInterval*2
}

// Invalidate 使缓存失效（用于账号变更时立即刷新）
func (p *AccountPool) Invalidate(ctx context.Context) {
	p.Refresh(ctx)
}

// SettingsCache 系统设置缓存
// @author ygw
type SettingsCache struct {
	settings    *models.Settings
	mu          sync.RWMutex
	lastRefresh time.Time
	ttl         time.Duration
	db          *database.DB
	refreshing  atomic.Bool
}

// NewSettingsCache 创建设置缓存
func NewSettingsCache(db *database.DB, ttl time.Duration) *SettingsCache {
	if ttl <= 0 {
		ttl = 30 * time.Second // 默认 30 秒
	}
	return &SettingsCache{
		db:  db,
		ttl: ttl,
	}
}

// Get 获取设置（自动刷新过期缓存）
func (c *SettingsCache) Get(ctx context.Context) (*models.Settings, error) {
	c.mu.RLock()
	if c.settings != nil && time.Since(c.lastRefresh) < c.ttl {
		settings := c.settings
		c.mu.RUnlock()
		return settings, nil
	}
	c.mu.RUnlock()

	// 需要刷新
	return c.refresh(ctx)
}

// refresh 刷新缓存
func (c *SettingsCache) refresh(ctx context.Context) (*models.Settings, error) {
	// 防止并发刷新
	if !c.refreshing.CompareAndSwap(false, true) {
		// 等待其他刷新完成
		c.mu.RLock()
		defer c.mu.RUnlock()
		if c.settings != nil {
			return c.settings, nil
		}
		return nil, nil
	}
	defer c.refreshing.Store(false)

	settings, err := c.db.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.settings = settings
	c.lastRefresh = time.Now()
	c.mu.Unlock()

	logger.Debug("设置缓存已刷新")
	return settings, nil
}

// Invalidate 使缓存失效
func (c *SettingsCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings = nil
	c.lastRefresh = time.Time{}
}

// Start 启动后台刷新任务
func (c *SettingsCache) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.ttl)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.refresh(ctx)
			}
		}
	}()
}

// TokenRefresher 令牌刷新器，使用 singleflight 模式避免重复刷新
// @author ygw - 高并发优化
type TokenRefresher struct {
	refreshing sync.Map // accountID -> *refreshState
}

// refreshState 刷新状态
type refreshState struct {
	mu       sync.Mutex
	done     bool
	err      error
	waiters  int       // 等待者数量
	doneTime time.Time // 完成时间
}

// NewTokenRefresher 创建令牌刷新器
func NewTokenRefresher() *TokenRefresher {
	return &TokenRefresher{}
}

// RefreshResult 刷新结果
type RefreshResult struct {
	Err      error
	Skipped  bool // 是否跳过（其他 goroutine 正在刷新）
	WasFirst bool // 是否是第一个发起刷新的
}

// TryRefresh 尝试刷新令牌
// doRefresh 是实际执行刷新的函数
// 返回: 刷新结果
// 如果同一账号正在被刷新，当前调用者会等待刷新完成并获得结果
func (r *TokenRefresher) TryRefresh(accountID string, doRefresh func() error) RefreshResult {
	// 加载或创建刷新状态
	stateI, loaded := r.refreshing.LoadOrStore(accountID, &refreshState{})
	state := stateI.(*refreshState)

	state.mu.Lock()

	// 检查是否刚刚完成刷新（5秒内不重复刷新）
	if state.done && time.Since(state.doneTime) < 5*time.Second {
		state.mu.Unlock()
		return RefreshResult{Err: state.err, Skipped: true, WasFirst: false}
	}

	// 判断是否需要执行刷新
	// 情况1: loaded=false（新创建）或 done=true（上次已完成，需要重新刷新）
	// 情况2: loaded=true 且 done=false（有人正在刷新，需要等待）
	if !loaded || state.done {
		// 我们来执行刷新（首次或重新刷新）
		state.done = false
		state.mu.Unlock()

		err := doRefresh()

		state.mu.Lock()
		state.done = true
		state.err = err
		state.doneTime = time.Now()
		state.mu.Unlock()

		return RefreshResult{Err: err, Skipped: false, WasFirst: true}
	}

	// 状态已存在且有人正在刷新，等待完成
	if !state.done {
		state.waiters++
		state.mu.Unlock()

		// 简单等待，最多等待 30 秒
		for i := 0; i < 300; i++ {
			time.Sleep(100 * time.Millisecond)
			state.mu.Lock()
			if state.done {
				state.waiters--
				err := state.err
				state.mu.Unlock()
				return RefreshResult{Err: err, Skipped: true, WasFirst: false}
			}
			state.mu.Unlock()
		}

		// 超时
		state.mu.Lock()
		state.waiters--
		state.mu.Unlock()
		return RefreshResult{Err: nil, Skipped: true, WasFirst: false}
	}

	state.mu.Unlock()
	return RefreshResult{Err: state.err, Skipped: true, WasFirst: false}
}

// ClearState 清除账号的刷新状态（用于账号被禁用等情况）
func (r *TokenRefresher) ClearState(accountID string) {
	r.refreshing.Delete(accountID)
}

// Cleanup 清理过期的刷新状态（超过 1 分钟）
func (r *TokenRefresher) Cleanup() {
	r.refreshing.Range(func(key, value interface{}) bool {
		state := value.(*refreshState)
		state.mu.Lock()
		defer state.mu.Unlock()

		if state.done && state.waiters == 0 && time.Since(state.doneTime) > time.Minute {
			r.refreshing.Delete(key)
		}
		return true
	})
}

// IPConfigCache IP配置缓存
// 用于缓存单个IP的频率限制配置，避免每次请求都查询数据库
// @author ygw
type IPConfigCache struct {
	cache sync.Map // key: IP (string), value: *ipConfigCacheEntry
	db    *database.DB
	ttl   time.Duration
}

// ipConfigCacheEntry IP配置缓存条目
type ipConfigCacheEntry struct {
	config   *models.IPConfig
	expireAt time.Time
}

// NewIPConfigCache 创建IP配置缓存
func NewIPConfigCache(db *database.DB, ttl time.Duration) *IPConfigCache {
	if ttl <= 0 {
		ttl = 60 * time.Second // 默认 60 秒
	}
	return &IPConfigCache{
		db:  db,
		ttl: ttl,
	}
}

// Get 获取IP配置（自动缓存）
// 返回值: config (可能为nil表示无配置), error
func (c *IPConfigCache) Get(ctx context.Context, ip string) (*models.IPConfig, error) {
	// 检查缓存
	if entry, ok := c.cache.Load(ip); ok {
		e := entry.(*ipConfigCacheEntry)
		if time.Now().Before(e.expireAt) {
			return e.config, nil
		}
	}

	// 从数据库加载
	config, err := c.db.GetIPConfig(ctx, ip)
	if err != nil {
		return nil, err
	}

	// 存入缓存
	c.cache.Store(ip, &ipConfigCacheEntry{
		config:   config,
		expireAt: time.Now().Add(c.ttl),
	})

	return config, nil
}

// Invalidate 使指定IP的缓存失效
func (c *IPConfigCache) Invalidate(ip string) {
	c.cache.Delete(ip)
}

// InvalidateAll 清空所有缓存
func (c *IPConfigCache) InvalidateAll() {
	c.cache.Range(func(key, value interface{}) bool {
		c.cache.Delete(key)
		return true
	})
}

// Cleanup 清理过期的缓存条目
func (c *IPConfigCache) Cleanup() {
	now := time.Now()
	c.cache.Range(func(key, value interface{}) bool {
		e := value.(*ipConfigCacheEntry)
		if now.After(e.expireAt) {
			c.cache.Delete(key)
		}
		return true
	})
}

// Start 启动后台清理任务
func (c *IPConfigCache) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // 每5分钟清理一次
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.Cleanup()
			}
		}
	}()
}
