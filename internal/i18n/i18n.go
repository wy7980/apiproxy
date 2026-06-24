// Package i18n provides internationalization support for apiproxy.
// It supports English (default) and Chinese (Simplified).
package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Language codes
const (
	EN = "en"     // English (default)
	ZH = "zh-CN"  // Simplified Chinese
)

var (
	instance *Translator
	once     sync.Once
)

// Translator handles message translation
type Translator struct {
	mu       sync.RWMutex
	lang     string
	messages map[string]map[string]string // lang -> key -> message
}

// LoadFunc is the type for loading translations
type LoadFunc func() (map[string]map[string]string, error)

// GetTranslator returns the singleton translator instance
func GetTranslator() *Translator {
	once.Do(func() {
		instance = &Translator{
			lang:     EN,
			messages: make(map[string]map[string]string),
		}
		// Load default embedded translations
		instance.messages = getEmbeddedTranslations()
	})
	return instance
}

// T translates a key to the current language
func T(key string, args ...interface{}) string {
	return GetTranslator().Translate(key, args...)
}

// TL translates a key to the specified language
func TL(lang, key string, args ...interface{}) string {
	return GetTranslator().TranslateLang(lang, key, args...)
}

// SetLanguage sets the default language
func SetLanguage(lang string) {
	GetTranslator().SetLang(lang)
}

// Translate translates a key to the current language
func (t *Translator) Translate(key string, args ...interface{}) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.translate(t.lang, key, args...)
}

// TranslateLang translates a key to the specified language
func (t *Translator) TranslateLang(lang, key string, args ...interface{}) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.translate(lang, key, args...)
}

func (t *Translator) translate(lang, key string, args ...interface{}) string {
	if lang == "" {
		lang = EN
	}
	langMap, ok := t.messages[lang]
	if !ok {
		langMap = t.messages[EN]
	}

	msg, ok := langMap[key]
	if !ok {
		// Fallback to English
		if enMsg, exists := t.messages[EN][key]; exists {
			msg = enMsg
		} else {
			msg = key // Return key if not found
		}
	}

	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// SetLang sets the default language
func (t *Translator) SetLang(lang string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Normalize language code
	switch strings.ToLower(lang) {
	case "zh", "zh-cn", "zh_cn":
		t.lang = ZH
	default:
		t.lang = EN
	}
}

// GetLang returns the current language
func (t *Translator) GetLang() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lang
}

// LoadMessages loads additional messages from a directory
func (t *Translator) LoadMessages(dir string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}

	for _, f := range files {
		lang := strings.TrimSuffix(filepath.Base(f), ".json")
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			continue
		}
		if _, ok := t.messages[lang]; !ok {
			t.messages[lang] = make(map[string]string)
		}
		for k, v := range msgs {
			t.messages[lang][k] = v
		}
	}
	return nil
}

// DetectFromEnv detects language from environment variables
func DetectFromEnv() string {
	// Check LANGUAGE first (highest priority)
	if lang := os.Getenv("LANGUAGE"); lang != "" {
		return normalizeLang(lang)
	}
	// Check LC_ALL
	if lang := os.Getenv("LC_ALL"); lang != "" {
		return normalizeLang(lang)
	}
	// Check LANG
	if lang := os.Getenv("LANG"); lang != "" {
		return normalizeLang(lang)
	}
	return EN
}

func normalizeLang(lang string) string {
	lang = strings.ToLower(lang)
	if strings.HasPrefix(lang, "zh") {
		return ZH
	}
	return EN
}

// getEmbeddedTranslations returns built-in translations
func getEmbeddedTranslations() map[string]map[string]string {
	return map[string]map[string]string{
		EN: {
			// Auth & Login
			"auth.invalid_credentials": "Invalid username or password",
			"auth.login": "Login",
			"auth.logout": "Logout",
			"auth.username": "Username",
			"auth.password": "Password",

			// Dashboard
			"dashboard.title": "apiproxy Performance Analytics",
			"dashboard.subtitle": "View model request volume, success rate, latency, tokens per second, and PP/TG speed across context lengths.",
			"dashboard.back": "Back to Dashboard",
			"dashboard.config": "Config",
			"dashboard.refresh": "Refresh",

			// Filters
			"dashboard.time_range": "Time Range",
			"dashboard.last_1h": "Last 1 hour",
			"dashboard.last_6h": "Last 6 hours",
			"dashboard.last_24h": "Last 24 hours",
			"dashboard.last_7d": "Last 7 days",
			"dashboard.last_30d": "Last 30 days",
			"dashboard.granularity": "Granularity",
			"dashboard.minute": "Minute",
			"dashboard.hour": "Hour",
			"dashboard.day": "Day",
			"dashboard.all": "All",

			// Table Headers
			"table.provider": "Provider",
			"table.model": "Model",
			"table.route": "Route",
			"table.requests": "Requests",
			"table.errors": "Errors",
			"table.success_rate": "Success Rate",
			"table.avg_latency": "Avg Latency",
			"table.p50": "P50",
			"table.p95": "P95",
			"table.p99": "P99",
			"table.tg_tps": "TG tok/s",
			"table.prompt": "Prompt",
			"table.completion": "Completion",
			"table.fallback": "Fallback",
			"table.total": "Total",
			"table.input_ratio": "Input %",
			"table.output_ratio": "Output %",

			// Chart Titles
			"chart.model_summary": "Model Performance Summary",
			"chart.token_totals": "Model Token Totals",
			"chart.latency_trend": "Latency Trend",
			"chart.tg_speed_trend": "Generation Speed Trend (TG)",
			"chart.pp_speed_by_ctx": "PP Speed by Context Length",
			"chart.tg_speed_by_ctx": "TG Speed by Context Length",

			// Config Management
			"config.title": "Config Management",
			"config.save": "Save",
			"config.add_provider": "+ Add Provider",

			// Common
			"common.loading": "Loading...",
			"common.error": "Error",
			"common.success": "Success",
			"common.confirm": "Confirm",
			"common.cancel": "Cancel",
		},
		ZH: {
			// Auth & Login
			"auth.invalid_credentials": "账号或密码错误",
			"auth.login": "登录",
			"auth.logout": "退出",
			"auth.username": "用户名",
			"auth.password": "密码",

			// Dashboard
			"dashboard.title": "apiproxy 性能分析",
			"dashboard.subtitle": "查看模型请求量、成功率、延迟、每秒 token、上下文长度下 PP/TG 速度。",
			"dashboard.back": "返回仪表盘",
			"dashboard.config": "配置",
			"dashboard.refresh": "刷新",

			// Filters
			"dashboard.time_range": "时间范围",
			"dashboard.last_1h": "最近 1 小时",
			"dashboard.last_6h": "最近 6 小时",
			"dashboard.last_24h": "最近 24 小时",
			"dashboard.last_7d": "最近 7 天",
			"dashboard.last_30d": "最近 30 天",
			"dashboard.granularity": "粒度",
			"dashboard.minute": "分钟",
			"dashboard.hour": "小时",
			"dashboard.day": "天",
			"dashboard.all": "全部",

			// Table Headers
			"table.provider": "Provider",
			"table.model": "Model",
			"table.route": "Route",
			"table.requests": "请求",
			"table.errors": "错误",
			"table.success_rate": "成功率",
			"table.avg_latency": "平均延迟",
			"table.p50": "P50",
			"table.p95": "P95",
			"table.p99": "P99",
			"table.tg_tps": "TG tok/s",
			"table.prompt": "Prompt",
			"table.completion": "Completion",
			"table.fallback": "Fallback",
			"table.total": "合计",
			"table.input_ratio": "输入占比",
			"table.output_ratio": "输出占比",

			// Chart Titles
			"chart.model_summary": "模型性能汇总",
			"chart.token_totals": "模型 Token 总量",
			"chart.latency_trend": "延迟趋势",
			"chart.tg_speed_trend": "生成速度趋势（TG）",
			"chart.pp_speed_by_ctx": "不同上下文长度 PP 速度",
			"chart.tg_speed_by_ctx": "不同上下文长度 TG 速度",

			// Config Management
			"config.title": "配置管理",
			"config.save": "保存",
			"config.add_provider": "+ 新增 Provider",

			// Common
			"common.loading": "加载中...",
			"common.error": "错误",
			"common.success": "成功",
			"common.confirm": "确认",
			"common.cancel": "取消",
		},
	}
}
