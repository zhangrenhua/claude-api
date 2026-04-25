package models

// Setting 表示数据库中的键值对设置
// 注意：使用 setting_key 而不是 key，因为 key 是 MySQL 保留字
type Setting struct {
	Key   string `gorm:"column:setting_key;primaryKey;size:100" json:"key"`
	Value string `gorm:"column:setting_value;type:text" json:"value"`
}

// TableName 指定表名
func (Setting) TableName() string {
	return "settings"
}

// AccountSelectionMode 账号选择方式常量
const (
	AccountSelectionSequential     = "sequential"      // 顺序选择（默认）
	AccountSelectionRandom         = "random"          // 随机选择
	AccountSelectionWeightedRandom = "weighted_random" // 加权随机选择
	AccountSelectionRoundRobin     = "round_robin"     // 轮询选择
	AccountSelectionCooldown       = "cooldown"        // 冷却时间选择
	AccountSelectionRPM            = "rpm"             // RPM 限制选择（每个账号 60s 滑动窗口内最多被调度 N 次，含失败）
)

// DefaultAccountCooldownSeconds 默认账号冷却时间（秒）
const DefaultAccountCooldownSeconds = 60

// DefaultAccountRPMLimit 默认每个账号 60 秒滑动窗口内最多被调度次数（含失败请求）
const DefaultAccountRPMLimit = 3

// DefaultAccountRPMFailureCooldownSeconds 默认请求失败后账号的冷却时长（秒）
const DefaultAccountRPMFailureCooldownSeconds = 90

// SupportedAccountSelectionModes 支持的账号选择方式列表
var SupportedAccountSelectionModes = []map[string]string{
	{"value": AccountSelectionSequential, "label": "顺序选择", "description": "按账号创建时间顺序选择"},
	{"value": AccountSelectionRandom, "label": "随机选择", "description": "随机选择一个账号"},
	{"value": AccountSelectionWeightedRandom, "label": "加权随机", "description": "根据配额剩余、使用时间等因素加权选择"},
	{"value": AccountSelectionRoundRobin, "label": "轮询选择", "description": "顺序轮流使用每个账号"},
	{"value": AccountSelectionCooldown, "label": "冷却时间", "description": "每个账号请求完成后需等待冷却时间后才能再被调度"},
	{"value": AccountSelectionRPM, "label": "RPM 限制", "description": "每个账号 60 秒滑动窗口内被调度次数达到上限后暂不再被调度（失败请求也计入额度，保证不击穿上游 RPM）"},
}

// Settings 表示系统配置（用于 API 响应）
type Settings struct {
	AdminPassword        string   `json:"adminPassword"`
	APIKey               string   `json:"apiKey"`
	DebugLog             bool     `json:"debugLog"`
	EnableRequestLog     bool     `json:"enableRequestLog"`
	LogRetentionDays     int      `json:"logRetentionDays"`
	EnableIPRateLimit    bool     `json:"enableIPRateLimit"`
	IPRateLimitWindow    int      `json:"ipRateLimitWindow"`
	IPRateLimitMax       int      `json:"ipRateLimitMax"`
	BlockedIPs           []string `json:"blockedIPs"`
	MaxErrorCount        int      `json:"maxErrorCount"`
	Port                 int      `json:"port"`
	PortConfigured       bool     `json:"-"` // 标记用户是否配置过端口（不序列化到JSON）
	LayoutFullWidth      bool     `json:"layoutFullWidth"`
	AccountSelectionMode    string   `json:"accountSelectionMode"`    // 账号选择方式: sequential, random, weighted_random, round_robin, cooldown, rpm
	AccountCooldownSeconds  int      `json:"accountCooldownSeconds"`  // 账号冷却时间（秒），cooldown 模式下生效
	AccountRPMLimit         int      `json:"accountRPMLimit"`         // 每个账号 60 秒滑动窗口内最多被调度次数（含失败），rpm 模式下生效
	AccountRPMFailureCooldownSeconds int `json:"accountRPMFailureCooldownSeconds"` // rpm 模式下请求失败后的账号冷却时长（秒）
	// 代理配置
	HTTPProxy string `json:"httpProxy"` // HTTP/HTTPS/SOCKS5 代理地址
	// 代理池配置
	ProxyPoolEnabled   bool   `json:"proxyPoolEnabled"`   // 是否启用代理池
	ProxyPoolStrategy  string `json:"proxyPoolStrategy"`  // 代理选择策略: round_robin, random, weighted
	// 智能压缩相关配置
	CompressionEnabled      bool   `json:"compressionEnabled"`      // 是否启用智能压缩
	CompressionModel        string `json:"compressionModel"`        // 压缩使用的模型
	CompressionTokenLimit   int    `json:"compressionTokenLimit"`   // 触发压缩的 token 阈值（0 使用默认值 180000）
	CompressionMessageLimit int    `json:"compressionMessageLimit"` // 触发压缩的消息数阈��（0 使用默认值 100）
	CompressionKeepMessages int    `json:"compressionKeepMessages"` // 压缩时保留最近的消息数（0 使用默认值 6）
	// 公告配置
	AnnouncementEnabled bool   `json:"announcementEnabled"` // 是否启用公告
	AnnouncementText    string `json:"announcementText"`    // 公告内容
	// 强制模型配置
	ForceModelEnabled bool   `json:"forceModelEnabled"` // 是否启用强制模型
	ForceModel        string `json:"forceModel"`        // 强制使用的模型
	// 性能优化配置（合并了配额刷新和状态检查）
	QuotaRefreshConcurrency int `json:"quotaRefreshConcurrency"` // 配额刷新并发数 (1-50)
	QuotaRefreshInterval    int `json:"quotaRefreshInterval"`    // 配额刷新间隔（秒，60-600）
}

// SettingsUpdate 表示更新设置的数据
type SettingsUpdate struct {
	AdminPassword        *string   `json:"adminPassword"`
	APIKey               *string   `json:"apiKey"`
	DebugLog             *bool     `json:"debugLog"`
	EnableRequestLog     *bool     `json:"enableRequestLog"`
	LogRetentionDays     *int      `json:"logRetentionDays"`
	EnableIPRateLimit    *bool     `json:"enableIPRateLimit"`
	IPRateLimitWindow    *int      `json:"ipRateLimitWindow"`
	IPRateLimitMax       *int      `json:"ipRateLimitMax"`
	BlockedIPs           *[]string `json:"blockedIPs"`
	MaxErrorCount        *int      `json:"maxErrorCount"`
	Port                 *int      `json:"port"`
	LayoutFullWidth      *bool     `json:"layoutFullWidth"`
	AccountSelectionMode   *string   `json:"accountSelectionMode"`   // 账号选择方式
	AccountCooldownSeconds *int      `json:"accountCooldownSeconds"` // 账号冷却时间（秒）
	AccountRPMLimit        *int      `json:"accountRPMLimit"`        // 60 秒滑动窗口内最多被调度次数（含失败）
	AccountRPMFailureCooldownSeconds *int `json:"accountRPMFailureCooldownSeconds"` // 失败冷却时长（秒）
	// 代理配置
	HTTPProxy *string `json:"httpProxy"`
	// 代理池配置
	ProxyPoolEnabled  *bool   `json:"proxyPoolEnabled"`
	ProxyPoolStrategy *string `json:"proxyPoolStrategy"`
	// 智能压缩相关配置
	CompressionEnabled      *bool   `json:"compressionEnabled"`
	CompressionModel        *string `json:"compressionModel"`
	CompressionTokenLimit   *int    `json:"compressionTokenLimit"`
	CompressionMessageLimit *int    `json:"compressionMessageLimit"`
	CompressionKeepMessages *int    `json:"compressionKeepMessages"`
	// 公告配置
	AnnouncementEnabled *bool   `json:"announcementEnabled"`
	AnnouncementText    *string `json:"announcementText"`
	// 强制模型配置
	ForceModelEnabled *bool   `json:"forceModelEnabled"`
	ForceModel        *string `json:"forceModel"`
	// 性能优化配置（合并了配额刷新和状态检查）
	QuotaRefreshConcurrency *int `json:"quotaRefreshConcurrency"`
	QuotaRefreshInterval    *int `json:"quotaRefreshInterval"`
}

// 支持的压缩模型列表
var SupportedCompressionModels = []string{
	"claude-haiku-4-5-20251001",
	"claude-sonnet-4-5-20250929",
	"claude-opus-4-5-20251101",
}

// 支持的强制模型列表（用于 API 请求）
var SupportedForceModels = []string{
	"claude-haiku-4-5-20251001",
	"claude-sonnet-4-5-20250929",
	"claude-opus-4-5-20251101",
}

// DefaultCompressionModel 默认压缩模型
const DefaultCompressionModel = "claude-sonnet-4-5-20250929"
