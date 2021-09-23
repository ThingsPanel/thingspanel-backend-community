package models

import "github.com/ThingsPanel/ThingsPanel-Go/domain/encoders"

// Response : defines the http API response structure
type Response interface {
	// Headers : return the response headers
	Headers() map[Header]interface{}
	// QueryParams : return a map of query string parameters
	QueryParams() map[QueryParam]interface{}
	// PathParams : returns a map of path parameters
	PathParams() map[PathParam]interface{}
	// SetHeader : set a response header in the response
	SetHeader(key Header, value interface{})
	// SetQueryParam : set a query parameter in the response
	SetQueryParam(key QueryParam, value string)
	// SetPathParam : set a path parameter in the response
	SetPathParam(key PathParam, value interface{})
	encoders.Encoder
}
