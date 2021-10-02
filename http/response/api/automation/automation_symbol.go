package automation

// SymbolResponseBody : automation symbol response body
type SymbolResponseBody []Symbol

// Symbol : symbol
type Symbol struct {
	// ID : identification code
	ID string `json:"id"`
	// Name : name string
	Name string `json:"name"`
}
