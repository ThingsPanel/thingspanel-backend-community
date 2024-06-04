package mqtt

type Config struct {
	Broker            string          `json:"broker"`
	User              string          `json:"user"`
	Pass              string          `json:"pass"`
	ChannelBufferSize int             `json:"channel_buffer_size"`
	WriteWorkers      int             `json:"write_workers"`
	Telemetry         Telemetry       `json:"telemetry"`
	Attributes        AttributeConfig `json:"attributes"`
	Commands          TopicConfig     `json:"commands"`
	Events            TopicConfig     `json:"events"`
	OTA               OTATopicConfig  `json:"ota"`
}

type Telemetry struct {
	SubscribeTopic        string `json:"subscribe_topic"`
	PublishTopic          string `json:"publish_topic"`
	GatewaySubscribeTopic string `json:"gateway_subscribe_topic"`
	GatewayPublishTopic   string `json:"gateway_publish_topic"`
	QoS                   int    `json:"qos"`
	PoolSize              int    `json:"pool_size"`
	BatchSize             int    `json:"batch_size"`
}

type TopicConfig struct {
	PublishTopic          string `json:"publish_topic"`
	SubscribeTopic        string `json:"subscribe_topic"`
	GatewaySubscribeTopic string `json:"gateway_subscribe_topic"`
	GatewayPublishTopic   string `json:"gateway_publish_topic"`
	QoS                   int    `json:"qos"`
}

type AttributeConfig struct {
	SubscribeTopic                string `json:"subscribe_topic"`
	GatewaySubscribeTopic         string `json:"gateway_subscribe_topic"`
	PublishResponseTopic          string `json:"publish_response_topic"`
	GatewayPublishResponseTopic   string `json:"gateway_publish_response_topic"`
	PublishTopic                  string `json:"publish_topic"`
	GatewayPublishTopic           string `json:"gateway_publish_topic"`
	SubscribeResponseTopic        string `json:"subscribe_response_topic"`
	GatewaySubscribeResponseTopic string `json:"gateway_subscribe_response_topic"`
	PublishGetTopic               string `json:"publish_get_topic"`
	GatewayPublishGetTopic        string `json:"gateway_publish_get_topic"`
	QoS                           int    `json:"qos"`
}

type OTATopicConfig struct {
	PublishTopic   string `json:"publish_topic"`
	SubscribeTopic string `json:"subscribe_topic"`
	QoS            int    `json:"qos"`
}
