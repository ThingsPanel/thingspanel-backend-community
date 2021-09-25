package auth

// PersonalInfoResponseBody : personal information of the user
type PersonalInfoResponseBody struct {
	// ID : user id
	ID string `json:"id"`
	// CreatedAt : time of creation as a unix timestamp
	CreatedAt string `json:"created_at"`
	// UpdatedAt : last updated time as a unix timestamp
	UpdatedAt string `json:"updated_at"`
	// Enabled : account active status
	Enabled bool `json:"enabled"`
	// AdditionalInfo : additional information, json style string
	AdditionalInfo interface{} `json:"additional_info"`
	// Authority : governing authority
	Authority interface{} `json:"authority"`
	// CustomerID : Customer identification number
	CustomerID string `json:"customer_id"`
	// Email : registered email of the user
	Email string `json:"email"`
	// Name : preferred username
	Name string `json:"name"`
	// FirstName : first name
	FirstName interface{} `json:"first_name"`
	// LastName : last name
	LastName interface{} `json:"last_name"`
	// SearchText : search text
	SearchText interface{} `json:"search_text"`
	// EmailVerifiedAt : email verification timestamp as a unix string
	EmailVerifiedAt string `json:"email_verified_at"`
	// Mobile : mobile number
	Mobile string `json:"mobile"`
	// Remark : remarks
	Remark string `json:"remark"`
	// IsAdmin : set 1 if the user is an admin , 0 otherwise
	IsAdmin int `json:"is_admin"`
	// BusinessID : uuid format string for business identification
	BusinessID string `json:"business_id"`
	// WXOpenID : wx open id
	WxOpenID string `json:"wx_openid"`
	// WXUnionID : wx union id
	WxUnionID string `json:"wx_unionid"`
}
