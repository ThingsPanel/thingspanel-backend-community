package communityclient

import (
	"context"
	"encoding/json"
	"net/url"
)

type ConnectionTopic struct {
	Purpose   string `json:"purpose"`
	Direction string `json:"direction"`
	Topic     string `json:"topic"`
}
type ConnectionTopics struct {
	Available      bool              `json:"available"`
	Reason         *string           `json:"reason"`
	MappingVersion *string           `json:"mappingVersion"`
	Items          []ConnectionTopic `json:"items"`
}

func (c *Client) GetConnectionTopics(ctx context.Context, id string) (*ConnectionTopics, error) {
	if _, ok := ctx.Value(authenticatedReadKey{}).(authenticatedRead); !ok {
		return nil, ErrRequestAuthentication
	}
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.doRaw(ctx, "GET", "/api/v1/device/connection/topics/"+url.PathEscape(id), token, nil)
	if err != nil {
		return nil, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return nil, err
	}
	var out ConnectionTopics
	if err = json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
