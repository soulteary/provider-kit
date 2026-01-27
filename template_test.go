package provider

import (
	"strings"
	"testing"
)

func TestFormatVerificationEmail(t *testing.T) {
	tests := []struct {
		name        string
		code        string
		locale      string
		wantSubject string
		wantBody    string
	}{
		{
			name:        "english",
			code:        "123456",
			locale:      "en-US",
			wantSubject: "Verification Code",
			wantBody:    "Your verification code is: 123456",
		},
		{
			name:        "chinese simplified",
			code:        "123456",
			locale:      "zh-CN",
			wantSubject: "验证码",
			wantBody:    "您的验证码是：123456",
		},
		{
			name:        "chinese traditional",
			code:        "654321",
			locale:      "zh-TW",
			wantSubject: "验证码",
			wantBody:    "您的验证码是：654321",
		},
		{
			name:        "japanese",
			code:        "111222",
			locale:      "ja-JP",
			wantSubject: "認証コード",
			wantBody:    "認証コード：111222",
		},
		{
			name:        "korean",
			code:        "333444",
			locale:      "ko-KR",
			wantSubject: "인증 코드",
			wantBody:    "인증 코드: 333444",
		},
		{
			name:        "german",
			code:        "555666",
			locale:      "de-DE",
			wantSubject: "Verifizierungscode",
			wantBody:    "Ihr Verifizierungscode lautet: 555666",
		},
		{
			name:        "french",
			code:        "777888",
			locale:      "fr-FR",
			wantSubject: "Code de vérification",
			wantBody:    "Votre code de vérification est : 777888",
		},
		{
			name:        "spanish",
			code:        "999000",
			locale:      "es-ES",
			wantSubject: "Código de verificación",
			wantBody:    "Su código de verificación es: 999000",
		},
		{
			name:        "default locale",
			code:        "112233",
			locale:      "",
			wantSubject: "Verification Code",
			wantBody:    "Your verification code is: 112233",
		},
		{
			name:        "unknown locale",
			code:        "445566",
			locale:      "xx-XX",
			wantSubject: "Verification Code",
			wantBody:    "Your verification code is: 445566",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, body := FormatVerificationEmail(tt.code, tt.locale)
			if subject != tt.wantSubject {
				t.Errorf("FormatVerificationEmail() subject = %v, want %v", subject, tt.wantSubject)
			}
			if !strings.Contains(body, tt.wantBody) {
				t.Errorf("FormatVerificationEmail() body = %v, should contain %v", body, tt.wantBody)
			}
		})
	}
}

func TestFormatVerificationSMS(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		locale string
		want   string
	}{
		{
			name:   "english",
			code:   "123456",
			locale: "en-US",
			want:   "Your verification code is: 123456",
		},
		{
			name:   "chinese",
			code:   "123456",
			locale: "zh-CN",
			want:   "您的验证码是：123456",
		},
		{
			name:   "japanese",
			code:   "111222",
			locale: "ja-JP",
			want:   "認証コード：111222",
		},
		{
			name:   "korean",
			code:   "333444",
			locale: "ko-KR",
			want:   "인증 코드: 333444",
		},
		{
			name:   "default",
			code:   "654321",
			locale: "",
			want:   "Your verification code is: 654321",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatVerificationSMS(tt.code, tt.locale)
			if !strings.Contains(got, tt.want) {
				t.Errorf("FormatVerificationSMS() = %v, should contain %v", got, tt.want)
			}
		})
	}
}

func TestBuildVerificationMessage(t *testing.T) {
	tests := []struct {
		name    string
		to      string
		code    string
		locale  string
		channel Channel
	}{
		{
			name:    "email",
			to:      "test@example.com",
			code:    "123456",
			locale:  "en-US",
			channel: ChannelEmail,
		},
		{
			name:    "sms",
			to:      "+1234567890",
			code:    "654321",
			locale:  "zh-CN",
			channel: ChannelSMS,
		},
		{
			name:    "unknown channel",
			to:      "test@example.com",
			code:    "111222",
			locale:  "en-US",
			channel: ChannelHTTP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := BuildVerificationMessage(tt.to, tt.code, tt.locale, tt.channel)
			if msg == nil {
				t.Fatal("BuildVerificationMessage() returned nil")
			}
			if msg.To != tt.to {
				t.Errorf("BuildVerificationMessage() To = %v, want %v", msg.To, tt.to)
			}
			if msg.Code != tt.code {
				t.Errorf("BuildVerificationMessage() Code = %v, want %v", msg.Code, tt.code)
			}
			if msg.Locale != tt.locale {
				t.Errorf("BuildVerificationMessage() Locale = %v, want %v", msg.Locale, tt.locale)
			}
			if msg.Params["code"] != tt.code {
				t.Errorf("BuildVerificationMessage() Params[code] = %v, want %v", msg.Params["code"], tt.code)
			}
			if tt.channel == ChannelEmail && msg.Subject == "" {
				t.Error("BuildVerificationMessage() Subject should not be empty for email")
			}
			if (tt.channel == ChannelEmail || tt.channel == ChannelSMS) && msg.Body == "" {
				t.Error("BuildVerificationMessage() Body should not be empty")
			}
		})
	}
}

func TestNewSimpleTemplateRenderer(t *testing.T) {
	renderer := NewSimpleTemplateRenderer()
	if renderer == nil {
		t.Fatal("NewSimpleTemplateRenderer() returned nil")
	}
	if renderer.leftDelim != "{{" {
		t.Errorf("leftDelim = %v, want {{", renderer.leftDelim)
	}
	if renderer.rightDelim != "}}" {
		t.Errorf("rightDelim = %v, want }}", renderer.rightDelim)
	}
}

func TestSimpleTemplateRenderer_WithDelimiters(t *testing.T) {
	renderer := NewSimpleTemplateRenderer().WithDelimiters("${", "}")

	if renderer.leftDelim != "${" {
		t.Errorf("leftDelim = %v, want ${", renderer.leftDelim)
	}
	if renderer.rightDelim != "}" {
		t.Errorf("rightDelim = %v, want }", renderer.rightDelim)
	}
}

func TestSimpleTemplateRenderer_Render(t *testing.T) {
	tests := []struct {
		name     string
		template string
		params   map[string]string
		want     string
	}{
		{
			name:     "simple replacement",
			template: "Hello, {{name}}!",
			params:   map[string]string{"name": "World"},
			want:     "Hello, World!",
		},
		{
			name:     "multiple replacements",
			template: "Your code is {{code}}. It expires in {{minutes}} minutes.",
			params: map[string]string{
				"code":    "123456",
				"minutes": "5",
			},
			want: "Your code is 123456. It expires in 5 minutes.",
		},
		{
			name:     "no params",
			template: "Hello, World!",
			params:   map[string]string{},
			want:     "Hello, World!",
		},
		{
			name:     "missing param",
			template: "Hello, {{name}}!",
			params:   map[string]string{},
			want:     "Hello, {{name}}!",
		},
		{
			name:     "repeated param",
			template: "{{code}} is your code. Remember: {{code}}",
			params:   map[string]string{"code": "123456"},
			want:     "123456 is your code. Remember: 123456",
		},
	}

	renderer := NewSimpleTemplateRenderer()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderer.Render(tt.template, tt.params)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Render() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSimpleTemplateRenderer_Render_CustomDelimiters(t *testing.T) {
	renderer := NewSimpleTemplateRenderer().WithDelimiters("${", "}")

	template := "Hello, ${name}!"
	params := map[string]string{"name": "World"}

	got, err := renderer.Render(template, params)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got != "Hello, World!" {
		t.Errorf("Render() = %v, want Hello, World!", got)
	}
}
