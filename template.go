package provider

import (
	"fmt"
	"strings"
)

// FormatVerificationEmail formats a verification code email (expiry fixed at 5 minutes).
// Returns subject and body based on locale. Use FormatVerificationEmailWithMinutes for custom expiry.
func FormatVerificationEmail(code string, locale string) (subject, body string) {
	return FormatVerificationEmailWithMinutes(code, locale, 5)
}

// FormatVerificationEmailWithMinutes formats a verification code email with custom expiry in minutes.
// Returns subject and body based on locale. Used by Herald template when purpose is "login".
func FormatVerificationEmailWithMinutes(code string, locale string, minutes int) (subject, body string) {
	if minutes <= 0 {
		minutes = 5
	}
	switch {
	case strings.HasPrefix(locale, "zh"):
		subject = "验证码"
		body = fmt.Sprintf("您的验证码是：%s\n\n此验证码将在%d分钟后过期。", code, minutes)
	case strings.HasPrefix(locale, "ja"):
		subject = "認証コード"
		body = fmt.Sprintf("認証コード：%s\n\nこのコードは%d分後に期限切れになります。", code, minutes)
	case strings.HasPrefix(locale, "ko"):
		subject = "인증 코드"
		body = fmt.Sprintf("인증 코드: %s\n\n이 코드는 %d분 후에 만료됩니다.", code, minutes)
	case strings.HasPrefix(locale, "de"):
		subject = "Verifizierungscode"
		body = fmt.Sprintf("Ihr Verifizierungscode lautet: %s\n\nDieser Code läuft in %d Minuten ab.", code, minutes)
	case strings.HasPrefix(locale, "fr"):
		subject = "Code de vérification"
		body = fmt.Sprintf("Votre code de vérification est : %s\n\nCe code expire dans %d minutes.", code, minutes)
	case strings.HasPrefix(locale, "es"):
		subject = "Código de verificación"
		body = fmt.Sprintf("Su código de verificación es: %s\n\nEste código expirará en %d minutos.", code, minutes)
	default:
		subject = "Verification Code"
		body = fmt.Sprintf("Your verification code is: %s\n\nThis code will expire in %d minutes.", code, minutes)
	}
	return subject, body
}

// FormatVerificationSMS formats a verification code SMS (expiry fixed at 5 minutes).
// Use FormatVerificationSMSWithMinutes for custom expiry.
func FormatVerificationSMS(code string, locale string) string {
	return FormatVerificationSMSWithMinutes(code, locale, 5)
}

// FormatVerificationSMSWithMinutes formats a verification code SMS with custom expiry in minutes.
// Used by Herald template when purpose is "login".
func FormatVerificationSMSWithMinutes(code string, locale string, minutes int) string {
	if minutes <= 0 {
		minutes = 5
	}
	switch {
	case strings.HasPrefix(locale, "zh"):
		return fmt.Sprintf("您的验证码是：%s，%d分钟内有效。", code, minutes)
	case strings.HasPrefix(locale, "ja"):
		return fmt.Sprintf("認証コード：%s、%d分間有効です。", code, minutes)
	case strings.HasPrefix(locale, "ko"):
		return fmt.Sprintf("인증 코드: %s, %d분간 유효합니다.", code, minutes)
	case strings.HasPrefix(locale, "de"):
		return fmt.Sprintf("Ihr Verifizierungscode: %s. Gültig für %d Minuten.", code, minutes)
	case strings.HasPrefix(locale, "fr"):
		return fmt.Sprintf("Votre code: %s. Valide %d minutes.", code, minutes)
	case strings.HasPrefix(locale, "es"):
		return fmt.Sprintf("Su código: %s. Válido por %d minutos.", code, minutes)
	default:
		return fmt.Sprintf("Your verification code is: %s. Valid for %d minutes.", code, minutes)
	}
}

// BuildVerificationMessage creates a verification code message
func BuildVerificationMessage(to, code, locale string, channel Channel) *Message {
	msg := NewMessage(to).
		WithCode(code).
		WithLocale(locale).
		WithParam("code", code)

	switch channel {
	case ChannelEmail:
		subject, body := FormatVerificationEmail(code, locale)
		msg.WithSubject(subject).WithBody(body)
	case ChannelSMS:
		body := FormatVerificationSMS(code, locale)
		msg.WithBody(body)
	}

	return msg
}

// TemplateRenderer renders templates with parameters
type TemplateRenderer interface {
	Render(template string, params map[string]string) (string, error)
}

// SimpleTemplateRenderer is a simple template renderer using string replacement
type SimpleTemplateRenderer struct {
	leftDelim  string
	rightDelim string
}

// NewSimpleTemplateRenderer creates a new simple template renderer
func NewSimpleTemplateRenderer() *SimpleTemplateRenderer {
	return &SimpleTemplateRenderer{
		leftDelim:  "{{",
		rightDelim: "}}",
	}
}

// WithDelimiters sets custom delimiters
func (r *SimpleTemplateRenderer) WithDelimiters(left, right string) *SimpleTemplateRenderer {
	r.leftDelim = left
	r.rightDelim = right
	return r
}

// Render renders a template with the given parameters
func (r *SimpleTemplateRenderer) Render(template string, params map[string]string) (string, error) {
	result := template
	for key, value := range params {
		placeholder := r.leftDelim + key + r.rightDelim
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result, nil
}
