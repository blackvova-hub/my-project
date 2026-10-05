package auth

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"mime"
	"net/smtp"
	"strings"
)

type MailerConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

type Mailer struct {
	cfg  MailerConfig
	auth smtp.Auth
}

func NewMailer(cfg MailerConfig) (*Mailer, error) {
	if cfg.Host == "" || cfg.Port == 0 || cfg.User == "" || cfg.Pass == "" || cfg.From == "" {
		return nil, fmt.Errorf("smtp_not_configured")
	}
	return &Mailer{cfg: cfg, auth: smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)}, nil
}

func (m *Mailer) SendVerification(to, code string) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	subject := "Код подтверждения регистрации"
	textBody := fmt.Sprintf(
		"Привет!\n\nВаш код подтверждения: %s\nКод действует 10 минут.\n\nЕсли вы не запрашивали регистрацию — просто проигнорируйте это письмо.",
		code,
	)
	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="ru">
  <body style="margin:0;background:#0b0b0c;color:#fff;font-family:-apple-system,Segoe UI,Roboto,Arial,sans-serif;">
    <div style="padding:24px;">
      <div style="max-width:520px;margin:0 auto;background:#141417;border:1px solid #2a2a2f;border-radius:16px;padding:24px;">
        <div style="font-size:12px;letter-spacing:1px;text-transform:uppercase;color:#9ea0ad;">short-and-long</div>
        <h2 style="margin:8px 0 12px;">Привет!</h2>
        <p style="margin:0 0 16px;color:#b8b8c2;">Подтверди регистрацию — введи код ниже.</p>
        <div style="font-size:32px;letter-spacing:6px;font-weight:700;margin:8px 0 16px;">%s</div>
        <p style="margin:0 0 12px;color:#b8b8c2;">Код действует 10 минут.</p>
        <p style="margin:0;color:#7e7e8a;font-size:12px;">Если вы не запрашивали регистрацию — просто проигнорируйте это письмо.</p>
      </div>
    </div>
  </body>
</html>`, code)

	msg := buildMessage(m.cfg.From, to, subject, textBody, htmlBody)

	return smtp.SendMail(addr, m.auth, m.cfg.From, []string{to}, []byte(msg))
}

func (m *Mailer) SendTwoFACode(to, code string) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	subject := "Код входа (2FA)"
	textBody := fmt.Sprintf(
		"Привет!\n\nВаш код входа: %s\nКод действует 10 минут.\n\nЕсли это не вы — смените пароль.",
		code,
	)
	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="ru">
  <body style="margin:0;background:#0b0b0c;color:#fff;font-family:-apple-system,Segoe UI,Roboto,Arial,sans-serif;">
    <div style="padding:24px;">
      <div style="max-width:520px;margin:0 auto;background:#141417;border:1px solid #2a2a2f;border-radius:16px;padding:24px;">
        <div style="font-size:12px;letter-spacing:1px;text-transform:uppercase;color:#9ea0ad;">short-and-long</div>
        <h2 style="margin:8px 0 12px;">Привет!</h2>
        <p style="margin:0 0 16px;color:#b8b8c2;">Введите код для входа в аккаунт.</p>
        <div style="font-size:32px;letter-spacing:6px;font-weight:700;margin:8px 0 16px;">%s</div>
        <p style="margin:0 0 12px;color:#b8b8c2;">Код действует 10 минут.</p>
        <p style="margin:0;color:#7e7e8a;font-size:12px;">Если это не вы — смените пароль.</p>
      </div>
    </div>
  </body>
</html>`, code)

	msg := buildMessage(m.cfg.From, to, subject, textBody, htmlBody)

	return smtp.SendMail(addr, m.auth, m.cfg.From, []string{to}, []byte(msg))
}

func (m *Mailer) SendPasswordResetLink(to, resetLink string) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	subject := "Восстановление пароля"
	textBody := fmt.Sprintf(
		"Вы запросили восстановление пароля. Чтобы задать новый пароль, перейдите по ссылке:\n%s\n\nСсылка действует 30 минут.\nЕсли вы не запрашивали восстановление, просто проигнорируйте это письмо.",
		resetLink,
	)
	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="ru">
  <body style="margin:0;background:#0b0b0c;color:#fff;font-family:-apple-system,Segoe UI,Roboto,Arial,sans-serif;">
    <div style="padding:24px;">
      <div style="max-width:520px;margin:0 auto;background:#141417;border:1px solid #2a2a2f;border-radius:16px;padding:24px;">
        <div style="font-size:12px;letter-spacing:1px;text-transform:uppercase;color:#9ea0ad;">short-and-long</div>
        <h2 style="margin:8px 0 12px;">Восстановление пароля</h2>
        <p style="margin:0 0 16px;color:#b8b8c2;">Чтобы задать новый пароль, перейдите по ссылке ниже.</p>
        <p style="margin:0 0 16px;"><a href="%s" style="color:#7dd3fc;word-break:break-all;">%s</a></p>
        <p style="margin:0 0 12px;color:#b8b8c2;">Ссылка действует 30 минут.</p>
        <p style="margin:0;color:#7e7e8a;font-size:12px;">Если вы не запрашивали восстановление, просто проигнорируйте это письмо.</p>
      </div>
    </div>
  </body>
</html>`, resetLink, resetLink)

	msg := buildMessage(m.cfg.From, to, subject, textBody, htmlBody)
	return smtp.SendMail(addr, m.auth, m.cfg.From, []string{to}, []byte(msg))
}

func buildMessage(from, to, subject, textBody, htmlBody string) string {
	boundary := randomBoundary()
	subject = encodeHeader(subject)
	parts := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"",
		"",
		"--" + boundary,
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		textBody,
		"",
		"--" + boundary,
		"Content-Type: text/html; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		htmlBody,
		"",
		"--" + boundary + "--",
	}
	return strings.Join(parts, "\r\n")
}

func encodeHeader(value string) string {
	if value == "" {
		return value
	}
	if strings.IndexFunc(value, func(r rune) bool { return r > 127 }) == -1 {
		return value
	}
	return mime.QEncoding.Encode("utf-8", value)
}

func randomBoundary() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "boundary"
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return "boundary-" + strings.ToLower(enc)
}

func GenerateNumericCode() (string, error) {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Base32 -> digits, берем 6 символов
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	enc = strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return '0' + rune((r-'A')%10)
		case r >= '2' && r <= '7':
			return '0' + rune((r-'2')%10)
		default:
			return -1
		}
	}, enc)
	if len(enc) < 6 {
		return "", fmt.Errorf("code_gen_failed")
	}
	return enc[:6], nil
}
