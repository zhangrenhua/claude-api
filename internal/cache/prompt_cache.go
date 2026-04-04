package cache

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// CacheTokenInfo 缓存 token 计算结果
type CacheTokenInfo struct {
	InputTokens              int // 非缓存部分的 input tokens
	CacheCreationInputTokens int // 新创建缓存的 tokens（已打折）
	CacheReadInputTokens     int // 从缓存读取的 tokens（已打折）
}

// CacheBreakpoint 缓存断点（由外部 token 计算时顺便收集）
type CacheBreakpoint struct {
	ContentHash string
	Tokens      int
}

// cacheEntry 缓存条目
type cacheEntry struct {
	createdAt time.Time
}

// PromptCache 本地 prompt 缓存追踪器
type PromptCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry // key: cache breakpoint hash
	ttl     time.Duration          // 缓存有效期（Claude 官方为 5 分钟）
}

// NewPromptCache 创建 prompt 缓存追踪器
func NewPromptCache(ttl time.Duration) *PromptCache {
	pc := &PromptCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
	}
	go pc.cleanup()
	return pc
}

// cleanup 定期清理过期缓存条目
func (pc *PromptCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		pc.mu.Lock()
		now := time.Now()
		for k, v := range pc.entries {
			if now.Sub(v.createdAt) > pc.ttl {
				delete(pc.entries, k)
			}
		}
		pc.mu.Unlock()
	}
}

// CalcCacheTokens 根据预收集的缓存断点计算缓存 token 信息
// totalInputTokens: 总输入 token 数（已由 countClaudeInputTokens 计算）
// cachedTokens: 带 cache_control 标记部分的 token 数（已由 countClaudeInputTokens 计算）
// breakpoints: 缓存断点列表（已由 countClaudeInputTokens 收集）
// 返回的 cache token 数打 3 折（模拟 prompt caching 的折扣效果）
func (pc *PromptCache) CalcCacheTokens(totalInputTokens, cachedTokens int, breakpoints []CacheBreakpoint) CacheTokenInfo {
	if len(breakpoints) == 0 {
		return CacheTokenInfo{InputTokens: totalInputTokens}
	}

	if cachedTokens > totalInputTokens {
		cachedTokens = totalInputTokens
	}

	// 生成缓存 key（基于所有缓存断点的内容 hash）
	cacheKey := buildCacheKey(breakpoints)

	// 检查是否已存在缓存（cache read vs cache creation）
	pc.mu.RLock()
	entry, exists := pc.entries[cacheKey]
	pc.mu.RUnlock()

	// cache 计数打 3 折返回（100 token → 33 token）
	discountedTokens := cachedTokens / 3

	result := CacheTokenInfo{
		InputTokens: totalInputTokens - cachedTokens,
	}

	if exists && time.Since(entry.createdAt) <= pc.ttl {
		// 缓存命中 → cache_read
		result.CacheReadInputTokens = discountedTokens
		// 刷新缓存时间（模拟 Claude 的缓存续期行为）
		pc.mu.Lock()
		entry.createdAt = time.Now()
		pc.mu.Unlock()
	} else {
		// 缓存未命中 → cache_creation
		result.CacheCreationInputTokens = discountedTokens
		pc.mu.Lock()
		pc.entries[cacheKey] = &cacheEntry{
			createdAt: time.Now(),
		}
		pc.mu.Unlock()
	}

	return result
}

// buildCacheKey 根据缓存断点构建缓存 key
func buildCacheKey(breakpoints []CacheBreakpoint) string {
	h := sha256.New()
	for _, bp := range breakpoints {
		h.Write([]byte(bp.ContentHash))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// HashContent 对文本内容生成 hash
func HashContent(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h[:16])
}

// HasCacheControl 检查 block 是否有 cache_control 标记
func HasCacheControl(blockMap map[string]interface{}) bool {
	cc, exists := blockMap["cache_control"]
	if !exists {
		return false
	}
	if ccMap, ok := cc.(map[string]interface{}); ok {
		t, _ := ccMap["type"].(string)
		return t == "ephemeral"
	}
	return false
}
