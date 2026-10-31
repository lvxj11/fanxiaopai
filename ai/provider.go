package ai

import (
	"context"
	"errors"
)

// ErrNotConfigured 还没有接入任何 AI 服务
var ErrNotConfigured = errors.New("未配置 AI 服务")

// SuggestRequest 让 AI 补充新菜时的上下文
type SuggestRequest struct {
	Slot        string   // 想换新的餐次
	Kind        string   // 想换新的类型（荤菜/素菜/…）
	AvoidTags   []string // 忌口标签
	KnownDishes []string // 菜库里已有的菜名，避免重复推荐
	Count       int      // 想要几道
}

// SuggestedDish AI 推荐的一道菜，字段与菜库对齐，用户确认后入库
type SuggestedDish struct {
	Name           string   `json:"name"`
	MainIngredient string   `json:"main_ingredient"`
	Kind           string   `json:"kind"`
	Slots          []string `json:"slots"`
	TimeCost       int      `json:"time_cost"`
	Green          bool     `json:"green"`
	Tags           []string `json:"tags"`
	Ingredients    []Item   `json:"ingredients"`
	Note           string   `json:"note"`
}

// Item AI 推荐的食材
type Item struct {
	Name string `json:"name"`
	Cat  string `json:"cat"`
	Qty  string `json:"qty"`
}

// Provider AI 菜谱来源。v1 只有 NoopProvider，
// 以后接真实模型（OpenAI 兼容接口等）时新增一个实现即可，调用方不用改
type Provider interface {
	// Configured 是否已可用，UI 用它决定"换新鲜"按钮能否点
	Configured() bool
	// Name 展示用的名字
	Name() string
	// Suggest 推荐菜库里没有的新菜
	Suggest(ctx context.Context, req SuggestRequest) ([]SuggestedDish, error)
}

// NoopProvider 未接入 AI 时的占位实现
type NoopProvider struct{}

// NewNoopProvider 创建占位实现
func NewNoopProvider() *NoopProvider { return &NoopProvider{} }

// Configured 永远为 false
func (p *NoopProvider) Configured() bool { return false }

// Name 展示名
func (p *NoopProvider) Name() string { return "未配置" }

// Suggest 直接返回 ErrNotConfigured
func (p *NoopProvider) Suggest(ctx context.Context, req SuggestRequest) ([]SuggestedDish, error) {
	return nil, ErrNotConfigured
}
