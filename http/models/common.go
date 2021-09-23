package models

// Header : defines the request headers
type Header string

func (h Header) String() string {
	return string(h)
}

// QueryParam : defines the request query string parameters
type QueryParam string

func (q QueryParam) String() string {
	return string(q)
}

// PathParam : defines the request path parameters
type PathParam string

func (p PathParam) String() string {
	return string(p)
}
