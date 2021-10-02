package pagination

// Info : pagination information
type Info struct {
	// CurrentPage : pagination current page
	CurrentPage int `json:"current_page"`
	// FirstPageURL : url of the first page
	FirstPageURL *string `json:"first_page_url"`
	// From : starting index of the paginated record set
	From int `json:"from"`
	// LastPage : last page
	LastPage int `json:"last_page"`
	// LastPageURL : url for the last page
	LastPageURL *string `json:"last_page_url"`
	// FirstPageURL : url of the first page
	NextPageURL *string `json:"next_page_url"`
	// Path : path of the request
	Path string `json:"path"`
	// PerPage : how many records per page
	PerPage int `json:"per_page"`
	// PrevPageURL : reference to the previous page
	PrevPageURL *string `json:"prev_page_url"`
	// To : ending index of the records
	To int `json:"to"`
	// Total : total number of records
	Total int `json:"total"`
}
