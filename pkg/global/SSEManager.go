package global

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"gopkg.in/redis.v5"
)

func InitSSEManager() {
	TPSSEManager = NewSSEManager()
	TPSSEManager.ListenForEvents()
}

type SSEManager struct {
	redisClient *redis.Client
	clients     map[string]map[string]*SSEClient // map[tenantID]map[userID]*SSEClient
	mutex       sync.RWMutex
}

type SSEClient struct {
	TenantID string
	UserID   string
	Writer   gin.ResponseWriter
	ClientID string
}

type SSEEvent struct {
	Type     string `json:"type"`
	Message  any    `json:"message"`
	TenantID string `json:"tenant_id"`
}

func NewSSEManager() *SSEManager {
	return &SSEManager{
		redisClient: REDIS,
		clients:     make(map[string]map[string]*SSEClient),
	}
}

func (m *SSEManager) AddClient(tenantID, userID string, writer gin.ResponseWriter) string {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	clientID := uuid.New()

	if _, ok := m.clients[tenantID]; !ok {
		m.clients[tenantID] = make(map[string]*SSEClient)
	}
	m.clients[tenantID][clientID] = &SSEClient{
		TenantID: tenantID,
		UserID:   userID,
		ClientID: clientID,
		Writer:   writer,
	}

	return clientID
}

func (m *SSEManager) RemoveClient(tenantID, clientID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if tenantClients, ok := m.clients[tenantID]; ok {
		delete(tenantClients, clientID)
		if len(tenantClients) == 0 {
			delete(m.clients, tenantID)
		}
	}
}

func (m *SSEManager) BroadcastEventToTenant(tenantID string, event SSEEvent) error {
	event.TenantID = tenantID
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return m.redisClient.Publish("sse:tenant:"+tenantID, string(eventJSON)).Err()
}

func (m *SSEManager) ListenForEvents() {
	pubsub, err := m.redisClient.PSubscribe("sse:tenant:*")
	if err != nil {
		logrus.Errorf("Failed to subscribe to Redis channel: %v", err)
		return
	}
	defer pubsub.Close()

	for {
		msg, err := pubsub.ReceiveMessage()
		if err != nil {
			logrus.Errorf("Error receiving message: %v", err)
			continue
		}

		var event SSEEvent
		if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
			logrus.Errorf("Failed to unmarshal event: %v", err)
			continue
		}

		m.mutex.RLock()
		tenantClients, ok := m.clients[event.TenantID]
		if ok {
			for _, client := range tenantClients {
				fmt.Fprintf(client.Writer, "event: %s\ndata: %s\n\n", event.Type, event.Message)
				client.Writer.(http.Flusher).Flush()
			}
		}
		m.mutex.RUnlock()
	}
}
