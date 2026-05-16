package auth

// Profile is a persisted Google-linked user record.
type Profile struct {
	GoogleID    string `json:"google_id"`
	Email       string `json:"email,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	DisplayName string `json:"display_name"`
	Updated     int64  `json:"updated,omitempty"` // unix seconds; set by store
}
