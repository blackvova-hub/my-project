package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	DB *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{DB: db}
}

func effectivePlanExpr(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return fmt.Sprintf("effective_plan(%splan, %ssubscription_expires_at, %ssubscription_frozen_at)", prefix, prefix, prefix)
}

func applySubscriptionState(u *User, expires pgtype.Timestamptz, frozen pgtype.Timestamptz, frozenDays int32) {
	if expires.Valid {
		t := expires.Time
		u.SubscriptionExpiresAt = &t
	}
	if frozen.Valid {
		t := frozen.Time
		u.SubscriptionFrozenAt = &t
	}
	if frozenDays < 0 {
		frozenDays = 0
	}
	u.SubscriptionFrozenDaysRemain = int(frozenDays)

	if frozen.Valid {
		u.SubscriptionDaysRemaining = int(frozenDays)
		u.SubscriptionActive = false
		return
	}

	now := time.Now()
	if expires.Valid && expires.Time.After(now) {
		rem := int(math.Ceil(expires.Time.Sub(now).Hours() / 24))
		if rem < 0 {
			rem = 0
		}
		u.SubscriptionDaysRemaining = rem
		plan := NormalizePlan(u.Plan)
		u.SubscriptionActive = rem > 0 && (plan == "standard" || plan == "pro")
		return
	}

	u.SubscriptionDaysRemaining = 0
	u.SubscriptionActive = false
}

func (s *Store) CreateUser(ctx context.Context, email string, passwordHash string) (User, error) {
	var u User
	var lastSeen pgtype.Timestamptz
	var subExpires pgtype.Timestamptz
	var subFrozenAt pgtype.Timestamptz
	var subFrozenDays int32
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		publicID, genErr := generatePublicID()
		if genErr != nil {
			return u, genErr
		}
		err = s.DB.QueryRow(ctx, `
			WITH ins AS (
				INSERT INTO users (email, password_hash, public_id, plan, subscription_expires_at)
				VALUES ($1, $2, $3, 'pro', now() + interval '30 days')
				RETURNING id, num_id, public_id, email, created_at
			)
			UPDATE users u
			SET display_name = 'User' || ins.public_id
			FROM ins
			WHERE u.id = ins.id
			RETURNING u.id::text, u.num_id, u.public_id, u.email, COALESCE(u.display_name, ''), `+effectivePlanExpr("u")+`, COALESCE(u.is_admin, false), u.two_fa_enabled, COALESCE(u.avatar_url, ''), COALESCE(u.primary_exchange, ''), u.email_verified, u.last_seen_at, u.subscription_expires_at, u.subscription_frozen_at, COALESCE(u.subscription_frozen_days_remaining, 0), u.created_at
		`, email, passwordHash, publicID).Scan(
			&u.ID,
			&u.NumID,
			&u.PublicID,
			&u.Email,
			&u.DisplayName,
			&u.Plan,
			&u.IsAdmin,
			&u.TwoFAEnabled,
			&u.AvatarURL,
			&u.PrimaryExchange,
			&u.EmailVerified,
			&lastSeen,
			&subExpires,
			&subFrozenAt,
			&subFrozenDays,
			&u.CreatedAt,
		)
		if err == nil {
			break
		}
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			if strings.Contains(pgErr.ConstraintName, "public_id") || strings.Contains(pgErr.ConstraintName, "idx_users_public_id") {
				continue
			}
		}
		break
	}
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	applySubscriptionState(&u, subExpires, subFrozenAt, subFrozenDays)
	return u, err
}

func generatePublicID() (int, error) {
	max := big.NewInt(9000000)
	value, err := rand.Int(rand.Reader, max)
	if err != nil {
		return 0, err
	}
	return int(value.Int64()) + 1000000, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, string, error) {
	var u User
	var passwordHash string
	var lastSeen pgtype.Timestamptz
	var subExpires pgtype.Timestamptz
	var subFrozenAt pgtype.Timestamptz
	var subFrozenDays int32
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, num_id, public_id, email, COALESCE(display_name, ''), `+effectivePlanExpr("")+`, COALESCE(is_admin, false), two_fa_enabled, COALESCE(avatar_url, ''), COALESCE(primary_exchange, ''), email_verified, last_seen_at, subscription_expires_at, subscription_frozen_at, COALESCE(subscription_frozen_days_remaining, 0), created_at, password_hash
		FROM users
		WHERE email = $1
	`, email).Scan(
		&u.ID,
		&u.NumID,
		&u.PublicID,
		&u.Email,
		&u.DisplayName,
		&u.Plan,
		&u.IsAdmin,
		&u.TwoFAEnabled,
		&u.AvatarURL,
		&u.PrimaryExchange,
		&u.EmailVerified,
		&lastSeen,
		&subExpires,
		&subFrozenAt,
		&subFrozenDays,
		&u.CreatedAt,
		&passwordHash,
	)
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	applySubscriptionState(&u, subExpires, subFrozenAt, subFrozenDays)
	return u, passwordHash, err
}

func (s *Store) GetUserBySessionID(ctx context.Context, sessionID string) (User, error) {
	tokenHash := hashSessionToken(sessionID)
	var u User
	var lastSeen pgtype.Timestamptz
	var subExpires pgtype.Timestamptz
	var subFrozenAt pgtype.Timestamptz
	var subFrozenDays int32
	err := s.DB.QueryRow(ctx, `
		WITH sid_user AS (
			SELECT user_id
			FROM sessions
			WHERE token_hash = $1
			  AND revoked_at IS NULL
			  AND expires_at > now()
			LIMIT 1
		), normalized AS (
			UPDATE users u0
			SET plan = 'free',
				subscription_expires_at = NULL,
				subscription_frozen_at = NULL,
				subscription_frozen_days_remaining = 0,
				updated_at = now()
			WHERE u0.id IN (SELECT user_id FROM sid_user)
			  AND lower(trim(COALESCE(u0.plan, 'free'))) IN ('standard', 'pro')
			  AND u0.subscription_frozen_at IS NULL
			  AND (u0.subscription_expires_at IS NULL OR u0.subscription_expires_at <= now())
			RETURNING u0.id
		)
		SELECT u.id::text, u.num_id, u.public_id, u.email, COALESCE(u.display_name, ''), `+effectivePlanExpr("u")+`, COALESCE(u.is_admin, false), u.two_fa_enabled, COALESCE(u.avatar_url, ''), COALESCE(u.primary_exchange, ''), u.email_verified, u.last_seen_at, u.subscription_expires_at, u.subscription_frozen_at, COALESCE(u.subscription_frozen_days_remaining, 0), u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > now()
	`, tokenHash).Scan(
		&u.ID,
		&u.NumID,
		&u.PublicID,
		&u.Email,
		&u.DisplayName,
		&u.Plan,
		&u.IsAdmin,
		&u.TwoFAEnabled,
		&u.AvatarURL,
		&u.PrimaryExchange,
		&u.EmailVerified,
		&lastSeen,
		&subExpires,
		&subFrozenAt,
		&subFrozenDays,
		&u.CreatedAt,
	)
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	applySubscriptionState(&u, subExpires, subFrozenAt, subFrozenDays)
	return u, err
}

func (s *Store) UpdateProfile(ctx context.Context, userID string, displayName, avatarURL, primaryExchange *string) (User, error) {
	sets := []string{}
	args := []any{}
	argN := 1

	if displayName != nil {
		sets = append(sets, "display_name = $"+itoa(argN))
		args = append(args, *displayName)
		argN++
	}
	if avatarURL != nil {
		sets = append(sets, "avatar_url = $"+itoa(argN))
		args = append(args, *avatarURL)
		argN++
	}
	if primaryExchange != nil {
		sets = append(sets, "primary_exchange = $"+itoa(argN))
		args = append(args, *primaryExchange)
		argN++
	}

	if len(sets) == 0 {
		return s.GetUserByID(ctx, userID)
	}

	args = append(args, userID)
	var u User
	var lastSeen pgtype.Timestamptz
	var subExpires pgtype.Timestamptz
	var subFrozenAt pgtype.Timestamptz
	var subFrozenDays int32
	q := "UPDATE users SET " + strings.Join(sets, ", ") + ", updated_at = now() WHERE id = $" + itoa(argN) + " RETURNING id::text, num_id, public_id, email, COALESCE(display_name, ''), " + effectivePlanExpr("") + ", COALESCE(is_admin, false), two_fa_enabled, COALESCE(avatar_url, ''), COALESCE(primary_exchange, ''), email_verified, last_seen_at, subscription_expires_at, subscription_frozen_at, COALESCE(subscription_frozen_days_remaining, 0), created_at"
	if err := s.DB.QueryRow(ctx, q, args...).Scan(
		&u.ID,
		&u.NumID,
		&u.PublicID,
		&u.Email,
		&u.DisplayName,
		&u.Plan,
		&u.IsAdmin,
		&u.TwoFAEnabled,
		&u.AvatarURL,
		&u.PrimaryExchange,
		&u.EmailVerified,
		&lastSeen,
		&subExpires,
		&subFrozenAt,
		&subFrozenDays,
		&u.CreatedAt,
	); err != nil {
		return User{}, err
	}
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	applySubscriptionState(&u, subExpires, subFrozenAt, subFrozenDays)
	return u, nil
}

func (s *Store) UpdateTwoFA(ctx context.Context, userID string, enabled bool) (User, error) {
	var u User
	var lastSeen pgtype.Timestamptz
	var subExpires pgtype.Timestamptz
	var subFrozenAt pgtype.Timestamptz
	var subFrozenDays int32
	err := s.DB.QueryRow(ctx, `
		UPDATE users
		SET two_fa_enabled = $1, updated_at = now()
		WHERE id = $2
		RETURNING id::text, num_id, public_id, email, COALESCE(display_name, ''), `+effectivePlanExpr("")+`, COALESCE(is_admin, false), two_fa_enabled, COALESCE(avatar_url, ''), COALESCE(primary_exchange, ''), email_verified, last_seen_at, subscription_expires_at, subscription_frozen_at, COALESCE(subscription_frozen_days_remaining, 0), created_at
	`, enabled, userID).Scan(
		&u.ID,
		&u.NumID,
		&u.PublicID,
		&u.Email,
		&u.DisplayName,
		&u.Plan,
		&u.IsAdmin,
		&u.TwoFAEnabled,
		&u.AvatarURL,
		&u.PrimaryExchange,
		&u.EmailVerified,
		&lastSeen,
		&subExpires,
		&subFrozenAt,
		&subFrozenDays,
		&u.CreatedAt,
	)
	if err != nil {
		return User{}, err
	}
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	applySubscriptionState(&u, subExpires, subFrozenAt, subFrozenDays)
	return u, nil
}

func (s *Store) UpdatePassword(ctx context.Context, userID string, passwordHash string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE users
		SET password_hash = $1, updated_at = now()
		WHERE id = $2
	`, passwordHash, userID)
	return err
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (User, error) {
	var u User
	var lastSeen pgtype.Timestamptz
	var subExpires pgtype.Timestamptz
	var subFrozenAt pgtype.Timestamptz
	var subFrozenDays int32
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, num_id, public_id, email, COALESCE(display_name, ''), `+effectivePlanExpr("")+`, COALESCE(is_admin, false), two_fa_enabled, COALESCE(avatar_url, ''), COALESCE(primary_exchange, ''), email_verified, last_seen_at, subscription_expires_at, subscription_frozen_at, COALESCE(subscription_frozen_days_remaining, 0), created_at
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&u.ID,
		&u.NumID,
		&u.PublicID,
		&u.Email,
		&u.DisplayName,
		&u.Plan,
		&u.IsAdmin,
		&u.TwoFAEnabled,
		&u.AvatarURL,
		&u.PrimaryExchange,
		&u.EmailVerified,
		&lastSeen,
		&subExpires,
		&subFrozenAt,
		&subFrozenDays,
		&u.CreatedAt,
	)
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	applySubscriptionState(&u, subExpires, subFrozenAt, subFrozenDays)
	return u, err
}

func (s *Store) GetWatchlists(ctx context.Context, userID string) ([]string, []string, error) {
	var rawHot []byte
	var rawCold []byte
	if err := s.DB.QueryRow(ctx, `SELECT watchlist_hot, watchlist_cold FROM users WHERE id = $1`, userID).Scan(&rawHot, &rawCold); err != nil {
		return nil, nil, err
	}
	var hot []string
	var cold []string
	if len(rawHot) > 0 {
		if err := json.Unmarshal(rawHot, &hot); err != nil {
			return nil, nil, err
		}
	}
	if len(rawCold) > 0 {
		if err := json.Unmarshal(rawCold, &cold); err != nil {
			return nil, nil, err
		}
	}
	return hot, cold, nil
}

func (s *Store) UpdateWatchlists(ctx context.Context, userID string, hot []string, cold []string) error {
	payloadHot, err := json.Marshal(hot)
	if err != nil {
		return err
	}
	payloadCold, err := json.Marshal(cold)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `UPDATE users SET watchlist_hot = $1, watchlist_cold = $2, updated_at = now() WHERE id = $3`, payloadHot, payloadCold, userID)
	return err
}

func (s *Store) TouchUser(ctx context.Context, userID string) error {
	_, err := s.DB.Exec(ctx, `UPDATE users SET last_seen_at = now(), updated_at = now() WHERE id = $1`, userID)
	return err
}

func (s *Store) SetEmailVerification(ctx context.Context, userID string, hash string, expires time.Time) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE users
		SET email_verify_hash = $1, email_verify_expires_at = $2
		WHERE id = $3
	`, hash, expires, userID)
	return err
}

func (s *Store) GetEmailVerificationByEmail(ctx context.Context, email string) (User, string, *time.Time, error) {
	var u User
	var expires pgtype.Timestamptz
	var lastSeen pgtype.Timestamptz
	var subExpires pgtype.Timestamptz
	var subFrozenAt pgtype.Timestamptz
	var subFrozenDays int32
	var hash string
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, num_id, public_id, email, COALESCE(display_name, ''), `+effectivePlanExpr("")+`, COALESCE(is_admin, false), two_fa_enabled, COALESCE(avatar_url, ''), COALESCE(primary_exchange, ''), email_verified, last_seen_at, subscription_expires_at, subscription_frozen_at, COALESCE(subscription_frozen_days_remaining, 0), created_at, COALESCE(email_verify_hash, ''), email_verify_expires_at
		FROM users
		WHERE email = $1
	`, email).Scan(
		&u.ID,
		&u.NumID,
		&u.PublicID,
		&u.Email,
		&u.DisplayName,
		&u.Plan,
		&u.IsAdmin,
		&u.TwoFAEnabled,
		&u.AvatarURL,
		&u.PrimaryExchange,
		&u.EmailVerified,
		&lastSeen,
		&subExpires,
		&subFrozenAt,
		&subFrozenDays,
		&u.CreatedAt,
		&hash,
		&expires,
	)
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	applySubscriptionState(&u, subExpires, subFrozenAt, subFrozenDays)
	var expPtr *time.Time
	if expires.Valid {
		expPtr = &expires.Time
	}
	return u, hash, expPtr, err
}

func (s *Store) MarkEmailVerified(ctx context.Context, userID string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE users
		SET email_verified = true, email_verify_hash = NULL, email_verify_expires_at = NULL, updated_at = now()
		WHERE id = $1
	`, userID)
	return err
}

func (s *Store) SetTwoFACode(ctx context.Context, userID, purpose, hash string, expires time.Time) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO two_fa_codes (user_id, purpose, code_hash, expires_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, purpose)
		DO UPDATE SET code_hash = EXCLUDED.code_hash, expires_at = EXCLUDED.expires_at, created_at = now()
	`, userID, purpose, hash, expires)
	return err
}

func (s *Store) GetTwoFACode(ctx context.Context, userID, purpose string) (string, *time.Time, error) {
	var hash string
	var expires pgtype.Timestamptz
	err := s.DB.QueryRow(ctx, `
		SELECT code_hash, expires_at
		FROM two_fa_codes
		WHERE user_id = $1 AND purpose = $2
	`, userID, purpose).Scan(&hash, &expires)
	if err != nil {
		return "", nil, err
	}
	if expires.Valid {
		return hash, &expires.Time, nil
	}
	return hash, nil, nil
}

func (s *Store) ClearTwoFACode(ctx context.Context, userID, purpose string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM two_fa_codes WHERE user_id = $1 AND purpose = $2`, userID, purpose)
	return err
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func (s *Store) CreateLoginChallenge(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	_, err := s.DB.Exec(ctx, `INSERT INTO login_challenges (token_hash, user_id, expires_at) VALUES ($1, $2::uuid, now() + interval '10 minutes')`, hashSessionToken(token), userID)
	return token, err
}

func (s *Store) ConsumeLoginChallenge(ctx context.Context, token, userID string) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM login_challenges WHERE token_hash = $1 AND user_id = $2::uuid AND expires_at > now()`, hashSessionToken(token), userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("invalid_login_challenge")
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration, userAgent string, ip string) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	sid := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := hashSessionToken(sid)
	var expiresAt time.Time
	ttlSeconds := int(ttl.Seconds())
	if ttlSeconds <= 0 {
		ttlSeconds = 60
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	err = tx.QueryRow(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at, user_agent, ip)
		VALUES ($1, $2::uuid, now() + ($3::int * interval '1 second'), $4, $5)
		RETURNING expires_at
	`, tokenHash, userID, ttlSeconds, userAgent, ip).Scan(&expiresAt)
	if err != nil {
		return "", time.Time{}, err
	}
	if strings.TrimSpace(ip) == "" {
		_, err = tx.Exec(ctx, `UPDATE users SET last_seen_at = now(), updated_at = now() WHERE id = $1`, userID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE users SET last_ip = $1, last_seen_at = now(), updated_at = now() WHERE id = $2`, ip, userID)
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", time.Time{}, err
	}
	return sid, expiresAt, nil
}

func (s *Store) GetSessionPublicID(ctx context.Context, sessionToken string) (string, error) {
	var id string
	err := s.DB.QueryRow(ctx, `SELECT public_id::text FROM sessions WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()`, hashSessionToken(sessionToken)).Scan(&id)
	return id, err
}

func (s *Store) ListSessionsForUser(ctx context.Context, userID string) ([]SessionInfo, error) {
	_, _ = s.DB.Exec(ctx, `
		DELETE FROM sessions
		WHERE user_id = $1
		  AND created_at < now() - interval '7 days'
	`, userID)

	rows, err := s.DB.Query(ctx, `
		SELECT public_id::text, COALESCE(user_agent, ''), COALESCE(ip, ''), created_at, expires_at,
		       (revoked_at IS NULL AND expires_at > now())
		FROM sessions
		WHERE user_id = $1
		  AND created_at >= now() - interval '7 days'
		ORDER BY created_at DESC
		LIMIT 20
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []SessionInfo{}
	for rows.Next() {
		var it SessionInfo
		if err := rows.Scan(&it.ID, &it.UserAgent, &it.IP, &it.CreatedAt, &it.ExpiresAt, &it.IsActive); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return items, nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hashSessionToken(sessionID))
	return err
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Store) CreateTelegramLinkToken(ctx context.Context, userID, token string, expiresAt time.Time) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO telegram_link_tokens (token, user_id, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (token) DO UPDATE
		SET user_id = EXCLUDED.user_id, expires_at = EXCLUDED.expires_at, created_at = now()
	`, token, userID, expiresAt)
	return err
}

func (s *Store) ConsumeTelegramLinkToken(ctx context.Context, token string) (string, error) {
	var userID string
	var expires pgtype.Timestamptz
	err := s.DB.QueryRow(ctx, `
		SELECT user_id::text, expires_at
		FROM telegram_link_tokens
		WHERE token = $1
	`, token).Scan(&userID, &expires)
	if err != nil {
		return "", err
	}
	if !expires.Valid || time.Now().After(expires.Time) {
		_, _ = s.DB.Exec(ctx, `DELETE FROM telegram_link_tokens WHERE token = $1`, token)
		return "", ErrUnauthorized
	}
	_, _ = s.DB.Exec(ctx, `DELETE FROM telegram_link_tokens WHERE token = $1`, token)
	return userID, nil
}

func (s *Store) SetTelegramLink(ctx context.Context, userID string, telegramID int64, username string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE users
		SET telegram_id = $1,
		    telegram_username = $2,
		    telegram_enabled = true,
		    telegram_linked_at = now(),
		    updated_at = now()
		WHERE id = $3
	`, telegramID, username, userID)
	return err
}

func (s *Store) UpdateTelegramEnabled(ctx context.Context, userID string, enabled bool) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE users
		SET telegram_enabled = $1, updated_at = now()
		WHERE id = $2
	`, enabled, userID)
	return err
}

func (s *Store) UpdateTelegramEnabledByTelegramID(ctx context.Context, telegramID int64, enabled bool) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE users
		SET telegram_enabled = $1, updated_at = now()
		WHERE telegram_id = $2
	`, enabled, telegramID)
	return err
}

func (s *Store) GetScannerSlotEnabledByTelegramID(ctx context.Context, telegramID int64, slot string) (bool, error) {
	var enabled bool
	err := s.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM alerts a
			JOIN users u ON u.num_id = a.user_id
			WHERE u.telegram_id = $1 AND a.scanner_slot = $2 AND a.enabled = true
		)
	`, telegramID, slot).Scan(&enabled)
	return enabled, err
}

func (s *Store) UpdateScannerSlotEnabledByTelegramID(ctx context.Context, telegramID int64, slot string, enabled bool) (int64, error) {
	cmd, err := s.DB.Exec(ctx, `
		UPDATE alerts
		SET enabled = $1, updated_at = now()
		WHERE user_id = (
			SELECT num_id FROM users WHERE telegram_id = $2
		)
		AND scanner_slot = $3
	`, enabled, telegramID, slot)
	if err != nil {
		return 0, err
	}
	return cmd.RowsAffected(), nil
}

func (s *Store) GetUserNumIDByTelegramID(ctx context.Context, telegramID int64) (int64, error) {
	var numID int64
	err := s.DB.QueryRow(ctx, `SELECT num_id FROM users WHERE telegram_id = $1`, telegramID).Scan(&numID)
	return numID, err
}

func (s *Store) GetUserByTelegramID(ctx context.Context, telegramID int64) (User, error) {
	var u User
	var lastSeen pgtype.Timestamptz
	var subExpires pgtype.Timestamptz
	var subFrozenAt pgtype.Timestamptz
	var subFrozenDays int32
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, num_id, public_id, email, COALESCE(display_name, ''), `+effectivePlanExpr("")+`,
			COALESCE(is_admin, false), two_fa_enabled,
			COALESCE(avatar_url, ''), COALESCE(primary_exchange, ''), email_verified, last_seen_at, subscription_expires_at, subscription_frozen_at, COALESCE(subscription_frozen_days_remaining, 0), created_at
		FROM users
		WHERE telegram_id = $1
	`, telegramID).Scan(
		&u.ID,
		&u.NumID,
		&u.PublicID,
		&u.Email,
		&u.DisplayName,
		&u.Plan,
		&u.IsAdmin,
		&u.TwoFAEnabled,
		&u.AvatarURL,
		&u.PrimaryExchange,
		&u.EmailVerified,
		&lastSeen,
		&subExpires,
		&subFrozenAt,
		&subFrozenDays,
		&u.CreatedAt,
	)
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	applySubscriptionState(&u, subExpires, subFrozenAt, subFrozenDays)
	return u, err
}

func (s *Store) UpsertTelegramSignalLimit(ctx context.Context, userID int64, symbol string, limit int) error {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil
	}
	if limit <= 0 {
		_, err := s.DB.Exec(ctx, `DELETE FROM telegram_signal_limits WHERE user_id = $1 AND symbol = $2`, userID, symbol)
		return err
	}
	_, err := s.DB.Exec(ctx, `
		INSERT INTO telegram_signal_limits (user_id, symbol, daily_limit, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (user_id, symbol)
		DO UPDATE SET daily_limit = EXCLUDED.daily_limit, updated_at = now()
	`, userID, symbol, limit)
	return err
}

func (s *Store) GetTelegramSignalLimit(ctx context.Context, userID int64, symbol string) (int, bool, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return 0, false, nil
	}
	var limit int
	err := s.DB.QueryRow(ctx, `
		SELECT daily_limit
		FROM telegram_signal_limits
		WHERE user_id = $1 AND symbol = $2
	`, userID, symbol).Scan(&limit)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, err
	}
	return limit, true, nil
}

func (s *Store) UpsertTelegramLimitRequest(ctx context.Context, telegramID int64, step string, symbol string) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO telegram_limit_requests (telegram_id, step, symbol, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (telegram_id)
		DO UPDATE SET step = EXCLUDED.step, symbol = EXCLUDED.symbol, updated_at = now()
	`, telegramID, step, symbol)
	return err
}

func (s *Store) GetTelegramLimitRequest(ctx context.Context, telegramID int64) (string, string, bool, error) {
	var step string
	var symbol pgtype.Text
	err := s.DB.QueryRow(ctx, `
		SELECT step, symbol
		FROM telegram_limit_requests
		WHERE telegram_id = $1
	`, telegramID).Scan(&step, &symbol)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return step, symbol.String, true, nil
}

func (s *Store) ClearTelegramLimitRequest(ctx context.Context, telegramID int64) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM telegram_limit_requests WHERE telegram_id = $1`, telegramID)
	return err
}

func (s *Store) ClearTelegramLink(ctx context.Context, userID string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE users
		SET telegram_id = NULL,
		    telegram_username = NULL,
		    telegram_enabled = false,
		    telegram_linked_at = NULL,
		    updated_at = now()
		WHERE id = $1
	`, userID)
	return err
}

func (s *Store) GetTelegramStatus(ctx context.Context, userID string) (int64, string, bool, error) {
	var telegramID pgtype.Int8
	var username pgtype.Text
	var enabled bool
	err := s.DB.QueryRow(ctx, `
		SELECT telegram_id, telegram_username, telegram_enabled
		FROM users
		WHERE id = $1
	`, userID).Scan(&telegramID, &username, &enabled)
	if err != nil {
		return 0, "", false, err
	}
	var id int64
	if telegramID.Valid {
		id = telegramID.Int64
	}
	return id, username.String, enabled, nil
}

func (s *Store) GetTelegramEnabledByTelegramID(ctx context.Context, telegramID int64) (bool, error) {
	var enabled bool
	err := s.DB.QueryRow(ctx, `
		SELECT telegram_enabled
		FROM users
		WHERE telegram_id = $1
	`, telegramID).Scan(&enabled)
	if err != nil {
		return false, err
	}
	return enabled, nil
}

type PasswordResetToken struct {
	UserID    int64
	ExpiresAt time.Time
	UsedAt    *time.Time
}

func (s *Store) CreatePasswordResetToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `
		UPDATE password_reset_tokens
		SET used_at = now()
		WHERE user_id = $1 AND used_at IS NULL
	`, userID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, tokenHash, expiresAt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) GetPasswordResetToken(ctx context.Context, tokenHash string) (PasswordResetToken, error) {
	var out PasswordResetToken
	var usedAt pgtype.Timestamptz
	err := s.DB.QueryRow(ctx, `
		SELECT user_id, expires_at, used_at
		FROM password_reset_tokens
		WHERE token_hash = $1
	`, tokenHash).Scan(&out.UserID, &out.ExpiresAt, &usedAt)
	if usedAt.Valid {
		out.UsedAt = &usedAt.Time
	}
	return out, err
}

func (s *Store) MarkPasswordResetTokenUsed(ctx context.Context, tokenHash string) error {
	_, err := s.DB.Exec(ctx, `
		UPDATE password_reset_tokens
		SET used_at = now()
		WHERE token_hash = $1 AND used_at IS NULL
	`, tokenHash)
	return err
}

func (s *Store) ConsumePasswordResetToken(ctx context.Context, tokenHash string) (int64, error) {
	var userID int64
	err := s.DB.QueryRow(ctx, `
		UPDATE password_reset_tokens
		SET used_at = now()
		WHERE token_hash = $1
		  AND used_at IS NULL
		  AND expires_at > now()
		RETURNING user_id
	`, tokenHash).Scan(&userID)
	if err != nil {
		return 0, err
	}
	return userID, nil
}

func (s *Store) DeleteSessionsByUserNumID(ctx context.Context, userNumID int64) error {
	_, err := s.DB.Exec(ctx, `
		DELETE FROM sessions
		WHERE user_id = (SELECT id FROM users WHERE num_id = $1)
	`, userNumID)
	return err
}

func (s *Store) GetUserByNumID(ctx context.Context, userNumID int64) (User, error) {
	var u User
	var lastSeen pgtype.Timestamptz
	var subExpires pgtype.Timestamptz
	var subFrozenAt pgtype.Timestamptz
	var subFrozenDays int32
	err := s.DB.QueryRow(ctx, `
		SELECT id::text, num_id, public_id, email, COALESCE(display_name, ''), `+effectivePlanExpr("")+`, COALESCE(is_admin, false), two_fa_enabled, COALESCE(avatar_url, ''), COALESCE(primary_exchange, ''), email_verified, last_seen_at, subscription_expires_at, subscription_frozen_at, COALESCE(subscription_frozen_days_remaining, 0), created_at
		FROM users
		WHERE num_id = $1
	`, userNumID).Scan(
		&u.ID,
		&u.NumID,
		&u.PublicID,
		&u.Email,
		&u.DisplayName,
		&u.Plan,
		&u.IsAdmin,
		&u.TwoFAEnabled,
		&u.AvatarURL,
		&u.PrimaryExchange,
		&u.EmailVerified,
		&lastSeen,
		&subExpires,
		&subFrozenAt,
		&subFrozenDays,
		&u.CreatedAt,
	)
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	applySubscriptionState(&u, subExpires, subFrozenAt, subFrozenDays)
	return u, err
}
