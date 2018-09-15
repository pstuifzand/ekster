package auth

// Auther
type Auther interface {
	AuthTokenAccepted(header string, r *TokenResponse) bool
}

// TokenResponse is the information that we get back from the token endpoint of the user...
type TokenResponse struct {
	Me       string `json:"me"`
	ClientID string `json:"client_id"`
	Scope    string `json:"scope"`
	IssuedAt int64  `json:"issued_at"`
	Nonce    int64  `json:"nonce"`
}
