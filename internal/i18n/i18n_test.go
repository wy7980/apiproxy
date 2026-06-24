package i18n

import "testing"

func TestTranslate(t *testing.T) {
	tr := GetTranslator()

	tests := []struct {
		lang string
		key  string
		want string
	}{
		{EN, "auth.login", "Login"},
		{ZH, "auth.login", "登录"},
		{EN, "dashboard.title", "apiproxy Performance Analytics"},
		{ZH, "dashboard.title", "apiproxy 性能分析"},
		{EN, "auth.invalid_credentials", "Invalid username or password"},
		{ZH, "auth.invalid_credentials", "账号或密码错误"},
	}

	for _, tt := range tests {
		got := tr.TranslateLang(tt.lang, tt.key)
		if got != tt.want {
			t.Errorf("TranslateLang(%q, %q) = %q, want %q", tt.lang, tt.key, got, tt.want)
		}
	}
}

func TestFallbackToEnglish(t *testing.T) {
	tr := GetTranslator()
	// Unknown language should fall back to English
	got := tr.TranslateLang("fr", "auth.login")
	if got != "Login" {
		t.Errorf("Expected English fallback, got %q", got)
	}
}

func TestUnknownKeyReturnsKey(t *testing.T) {
	tr := GetTranslator()
	got := tr.TranslateLang(EN, "nonexistent.key")
	if got != "nonexistent.key" {
		t.Errorf("Expected key returned, got %q", got)
	}
}

func TestSetLanguage(t *testing.T) {
	tr := GetTranslator()

	tr.SetLang("zh-CN")
	if tr.GetLang() != ZH {
		t.Errorf("Expected %s, got %s", ZH, tr.GetLang())
	}

	tr.SetLang("zh")
	if tr.GetLang() != ZH {
		t.Errorf("Expected %s, got %s", ZH, tr.GetLang())
	}

	tr.SetLang("en")
	if tr.GetLang() != EN {
		t.Errorf("Expected %s, got %s", EN, tr.GetLang())
	}

	// Unknown language defaults to English
	tr.SetLang("fr")
	if tr.GetLang() != EN {
		t.Errorf("Expected %s, got %s", EN, tr.GetLang())
	}
}

func TestNormalizeLang(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"zh-CN", ZH},
		{"zh-cn", ZH},
		{"zh", ZH},
		{"ZH", ZH},
		{"en-US", EN},
		{"en", EN},
		{"fr", EN},
		{"", EN},
	}

	for _, tt := range tests {
		got := normalizeLang(tt.input)
		if got != tt.want {
			t.Errorf("normalizeLang(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatArgs(t *testing.T) {
	tr := GetTranslator()
	// Test with format args (if any message uses them)
	got := tr.TranslateLang(EN, "auth.invalid_credentials")
	if got == "" {
		t.Error("Expected non-empty translation")
	}
}
