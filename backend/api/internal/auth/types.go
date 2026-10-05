package auth

import "time"

type User struct {
	ID                           string     `json:"id"`
	NumID                        int64      `json:"num_id"`
	PublicID                     int64      `json:"public_id"`
	Email                        string     `json:"email"`
	DisplayName                  string     `json:"displayName"`
	Plan                         string     `json:"plan"`
	SubscriptionExpiresAt        *time.Time `json:"subscriptionExpiresAt,omitempty"`
	SubscriptionFrozenAt         *time.Time `json:"subscriptionFrozenAt,omitempty"`
	SubscriptionFrozenDaysRemain int        `json:"subscriptionFrozenDaysRemaining"`
	SubscriptionDaysRemaining    int        `json:"subscriptionDaysRemaining"`
	SubscriptionActive           bool       `json:"subscriptionActive"`
	IsAdmin                      bool       `json:"isAdmin"`
	TwoFAEnabled                 bool       `json:"twoFAEnabled"`
	AvatarURL                    string     `json:"avatarUrl"`
	PrimaryExchange              string     `json:"primaryExchange"`
	EmailVerified                bool       `json:"emailVerified"`
	LastSeenAt                   *time.Time `json:"lastSeenAt"`
	CreatedAt                    time.Time  `json:"createdAt"`
}

type SessionInfo struct {
	ID        string    `json:"id"`
	UserAgent string    `json:"userAgent"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	IsActive  bool      `json:"isActive"`
}

type LoginChallenge struct {
	UserID    string
	ExpiresAt time.Time
}
