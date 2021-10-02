package automation

type StatusResponseBody []Status

// Status : automation status
type Status struct {
	// ID : identification code
	ID int `json:"id"`
	// Name : name string
	Name string `json:"name"`
}
