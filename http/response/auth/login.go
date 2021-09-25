package auth

// LoginResponseBody : response body of the login request
type LoginResponseBody struct {
	// AccessToken: access/auth token
	AccessToken string `json:"access_token"`
	// TokenType: type of the token
	TokenType string `json:"token_type"`
	// Expiry: expiry period of the token in seconds
	Expiry int64 `json:"expiry"`
}
