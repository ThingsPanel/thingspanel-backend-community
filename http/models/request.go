package models

import "github.com/ThingsPanel/ThingsPanel-Go/domain/encoders"

// Request : defines the http API request structure
type Request interface {
	// Headers : return the request headers
	Headers() map[Header]interface{}
	// QueryParams : return a map of query string parameters
	QueryParams() map[QueryParam]interface{}
	// PathParams : returns a map of path parameters
	PathParams() map[PathParam]interface{}
	// SetHeader : set a request header in the request
	SetHeader(key Header, value interface{})
	// SetQueryParam : set a query parameter in the request
	SetQueryParam(key QueryParam, value string)
	// SetPathParam : set a path parameter in the request
	SetPathParam(key PathParam, value interface{})
	encoders.Encoder
}
