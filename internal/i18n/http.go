package i18n

import (
	"html/template"
	"net/http"
	"strings"
)

// TemplateFuncs returns i18n template functions
func TemplateFuncs(lang string) template.FuncMap {
	return template.FuncMap{
		"t": func(key string, args ...interface{}) string {
			return TL(lang, key, args...)
		},
		"tlang": func() string {
			return lang
		},
	}
}

// DetectFromRequest detects language from HTTP request
// Priority: 1. Cookie  2. Accept-Language header  3. Default (en)
func DetectFromRequest(r *http.Request) string {
	// Check cookie first
	if cookie, err := r.Cookie("lang"); err == nil && cookie.Value != "" {
		return normalizeLang(cookie.Value)
	}

	// Check Accept-Language header
	if acceptLang := r.Header.Get("Accept-Language"); acceptLang != "" {
		// Parse simple form - just check prefix
		if strings.HasPrefix(strings.ToLower(acceptLang), "zh") {
			return ZH
		}
	}

	return EN
}

// SetLanguageCookie sets the language cookie
func SetLanguageCookie(w http.ResponseWriter, lang string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "lang",
		Value:    lang,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60, // 1 year
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
