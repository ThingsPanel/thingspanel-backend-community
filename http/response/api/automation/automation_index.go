package automation

import "github.com/ThingsPanel/ThingsPanel-Go/http/response/pagination"

// IndexResponseBody : automation index response body
type IndexResponseBody struct {
	pagination.Info
	Data []interface{} `json:"data"` // todo - index response body
}
