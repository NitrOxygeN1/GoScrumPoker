package auth

// Profile is a persisted Google-linked user record.
type Profile struct {
	ID      string `json:"id"`      // Google subject (stable user id)
	Name    string `json:"name"`
	Avatar  string `json:"avatar,omitempty"`
	Email   string `json:"email,omitempty"`
	Updated int64  `json:"updated"` // unix seconds; set by store
}
