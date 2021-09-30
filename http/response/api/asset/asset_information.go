package asset

// InfoResponseBody : asset information response
type InfoResponseBody []Info

// AssetInfo : asst information
type Info struct {
	// ID : Asset id alphanumeric
	ID string `json:"id"`
	// Name : Asset name
	Name string `json:"name"`
}
