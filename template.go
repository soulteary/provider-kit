package provider

import (
	"fmt"
	"strings"
)

// FormatVerificationEmail formats a verification code email
// Returns subject and body based on locale
func FormatVerificationEmail(code string, locale string) (subject, body string) {
	switch {
	case strings.HasPrefix(locale, "zh"):
		subject = "验证码"
		body = fmt.Sprintf("您的验证码是：%s\n\n此验证码将在5分钟后过期。", code)
	case strings.HasPrefix(locale, "ja"):
		subject = "認証コード"
		body = fmt.Sprintf("認証コード：%s\n\nこのコードは5分後に期限切れになります。", code)
	case strings.HasPrefix(locale, "ko"):
		subject = "인증 코드"
		body = fmt.Sprintf("인증 코드: %s\n\n이 코드는 5분 후에 만료됩니다.", code)
	case strings.HasPrefix(locale, "de"):
		subject = "Verifizierungscode"
		body = fmt.Sprintf("Ihr Verifizierungscode lautet: %s\n\nDieser Code läuft in 5 Minuten ab.", code)
	case strings.HasPrefix(locale, "fr"):
		subject = "Code de vérification"
		body = fmt.Sprintf("Votre code de vérification est : %s\n\nCe code expire dans 5 minutes.", code)
	case strings.HasPrefix(locale, "es"):
		subject = "Código de verificación"
		body = fmt.Sprintf("Su código de verificación es: %s\n\nEste código expirará en 5 minutos.", code)
	default:
		subject = "Verification Code"
		body = fmt.Sprintf("Your verification code is: %s\n\nThis code will expire in 5 minutes.", code)
	}
	return subject, body
}

// FormatVerificationSMS formats a verification code SMS
func FormatVerificationSMS(code string, locale string) string {
	switch {
	case strings.HasPrefix(locale, "zh"):
		return fmt.Sprintf("您的验证码是：%s，5分钟内有效。", code)
	case strings.HasPrefix(locale, "ja"):
		return fmt.Sprintf("認証コード：%s、5分間有効です。", code)
	case strings.HasPrefix(locale, "ko"):
		return fmt.Sprintf("인증 코드: %s, 5분간 유효합니다.", code)
	case strings.HasPrefix(locale, "de"):
		return fmt.Sprintf("Ihr Verifizierungscode: %s. Gültig für 5 Minuten.", code)
	case strings.HasPrefix(locale, "fr"):
		return fmt.Sprintf("Votre code: %s. Valide 5 minutes.", code)
	case strings.HasPrefix(locale, "es"):
		return fmt.Sprintf("Su código: %s. Válido por 5 minutos.", code)
	default:
		return fmt.Sprintf("Your verification code is: %s. Valid for 5 minutes.", code)
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
