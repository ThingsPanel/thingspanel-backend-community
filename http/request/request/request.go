package request

import "github.com/ThingsPanel/ThingsPanel-Go/http/models"

type Request struct {
	headers map[models.Header]interface{}
	query   map[models.QueryParam]interface{}
	path    map[models.PathParam]interface{}
}

func (r *Request) Headers() map[models.Header]interface{} {
	return r.headers
}

func (r *Request) QueryParams() map[models.QueryParam]interface{} {
	return r.query
}

func (r *Request) PathParams() map[models.PathParam]interface{} {
	return r.path
}

func (r *Request) SetHeader(key models.Header, value interface{}) {
	r.headers[key] = value
}

func (r *Request) SetQueryParam(key models.QueryParam, value string) {
	r.query[key] = value
}

func (r Request) SetPathParam(key models.PathParam, value interface{}) {
	r.path[key] = value
}
