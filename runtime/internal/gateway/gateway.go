package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/duxweb/dux-runtime/runtime/internal/phpmaster"
	"github.com/duxweb/dux-runtime/runtime/internal/status"
	"github.com/olahol/melody"
)

type Service struct {
	melody *melody.Melody
	master *phpmaster.Client
	state  *status.State

	mu      sync.RWMutex
	clients map[string]*Client
	topics  map[string]map[string]struct{}
}

type Client struct {
	Session        *melody.Session
	App            string
	ID             string
	Type           string
	Meta           map[string]any
	AllowSubscribe map[string][]string
	AllowPublish   map[string][]string
	Topics         map[string]struct{}
}

type Envelope struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Topic     string         `json:"topic"`
	Target    string         `json:"target"`
	Payload   map[string]any `json:"payload,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
	ReplyTo   string         `json:"reply_to"`
	Timestamp int64          `json:"timestamp"`
}

func New(master *phpmaster.Client, state *status.State) *Service {
	g := &Service{
		melody:  melody.New(),
		master:  master,
		state:   state,
		clients: map[string]*Client{},
		topics:  map[string]map[string]struct{}{},
	}
	g.bind()
	return g
}

func (g *Service) HandleWS(writer http.ResponseWriter, request *http.Request) {
	_ = g.melody.HandleRequestWithKeys(writer, request, map[string]any{})
}

func (g *Service) HandleClients(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"ok":    true,
		"items": g.clientsSnapshot(),
	})
}

func (g *Service) HandleTopics(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"ok":    true,
		"items": g.topicsSnapshot(),
	})
}

func (g *Service) HandlePublish(writer http.ResponseWriter, request *http.Request) {
	var envelope Envelope
	if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	g.publishToTopic(envelope.Topic, envelope)
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
}

func (g *Service) HandlePushClient(writer http.ResponseWriter, request *http.Request) {
	var envelope Envelope
	if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if envelope.Target == "" {
		http.Error(writer, "target is required", http.StatusBadRequest)
		return
	}
	_ = g.pushClient(envelope.Target, envelope)
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
}

func (g *Service) HandleKick(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if payload.ClientID == "" {
		http.Error(writer, "client_id is required", http.StatusBadRequest)
		return
	}
	_ = g.kick(payload.ClientID)
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
}

func (g *Service) bind() {
	g.melody.HandleConnect(func(session *melody.Session) {
		request := session.Request
		auth, err := g.master.WsAuth(request.Context(), phpmaster.WsAuthRequest{
			App:   request.URL.Query().Get("app"),
			Token: request.URL.Query().Get("token"),
			Meta: map[string]any{
				"client_ip":  request.RemoteAddr,
				"user_agent": request.UserAgent(),
				"query":      request.URL.Query(),
			},
		})
		if err != nil || auth == nil || auth.ClientID == "" {
			_ = session.Close()
			return
		}

		client := &Client{
			Session:        session,
			App:            request.URL.Query().Get("app"),
			ID:             auth.ClientID,
			Type:           auth.ClientType,
			Meta:           auth.Meta,
			AllowSubscribe: auth.AllowSubscribe,
			AllowPublish:   auth.AllowPublish,
			Topics:         map[string]struct{}{},
		}
		session.Set("client_id", client.ID)

		_ = g.kick(client.ID)
		g.mu.Lock()
		g.clients[client.ID] = client
		g.mu.Unlock()
		g.syncState()
		_, _ = g.master.WsEvent(context.Background(), phpmaster.WsGatewayEvent{
			Event:    "online",
			App:      client.App,
			ClientID: client.ID,
			Meta:     client.Meta,
		})
	})

	g.melody.HandleDisconnect(func(session *melody.Session) {
		client := g.findBySession(session)
		if client == nil {
			return
		}
		g.removeClient(client.ID)
		g.syncState()
		_, _ = g.master.WsEvent(context.Background(), phpmaster.WsGatewayEvent{
			Event:    "offline",
			App:      client.App,
			ClientID: client.ID,
			Meta:     client.Meta,
		})
	})

	g.melody.HandleMessage(func(session *melody.Session, data []byte) {
		client := g.findBySession(session)
		if client == nil {
			return
		}

		var envelope Envelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			_ = g.write(session, map[string]any{"type": "error", "error": err.Error()})
			return
		}
		if envelope.Timestamp == 0 {
			envelope.Timestamp = time.Now().Unix()
		}

		switch envelope.Type {
		case "subscribe":
			g.handleSubscribe(client, envelope)
		case "unsubscribe":
			g.handleUnsubscribe(client, envelope)
		case "publish":
			g.handlePublish(client, envelope)
		case "ping":
			_, _ = g.master.WsEvent(context.Background(), phpmaster.WsGatewayEvent{
				Event:    "ping",
				App:      client.App,
				ClientID: client.ID,
				Meta:     client.Meta,
			})
			_ = g.write(client.Session, map[string]any{"type": "pong", "id": envelope.ID, "timestamp": time.Now().Unix()})
		default:
			_ = g.write(client.Session, map[string]any{"type": "error", "id": envelope.ID, "error": "message type not supported"})
		}
	})
}

func (g *Service) handleSubscribe(client *Client, envelope Envelope) {
	if envelope.Topic == "" {
		_ = g.write(client.Session, map[string]any{"type": "error", "id": envelope.ID, "error": "topic is required"})
		return
	}
	if !g.allowTopic(client.AllowSubscribe, envelope.Topic) {
		_ = g.write(client.Session, map[string]any{"type": "error", "id": envelope.ID, "error": "subscribe not allowed"})
		return
	}
	resp, err := g.master.WsSubscribe(context.Background(), phpmaster.WsActionRequest{
		App:      client.App,
		ClientID: client.ID,
		Topic:    envelope.Topic,
		Payload:  envelope.Payload,
		Meta:     envelope.Meta,
	})
	if err == nil && resp != nil && !resp.Allow {
		_ = g.write(client.Session, map[string]any{"type": "error", "id": envelope.ID, "error": "subscribe denied"})
		return
	}

	g.mu.Lock()
	if _, ok := g.topics[envelope.Topic]; !ok {
		g.topics[envelope.Topic] = map[string]struct{}{}
	}
	g.topics[envelope.Topic][client.ID] = struct{}{}
	client.Topics[envelope.Topic] = struct{}{}
	g.mu.Unlock()
	g.syncState()
	if g.state != nil {
		g.state.IncWSSubscribed()
	}
	_ = g.write(client.Session, map[string]any{"type": "ack", "id": envelope.ID, "topic": envelope.Topic})
}

func (g *Service) handleUnsubscribe(client *Client, envelope Envelope) {
	if envelope.Topic == "" {
		_ = g.write(client.Session, map[string]any{"type": "error", "id": envelope.ID, "error": "topic is required"})
		return
	}
	g.mu.Lock()
	delete(client.Topics, envelope.Topic)
	if members, ok := g.topics[envelope.Topic]; ok {
		delete(members, client.ID)
		if len(members) == 0 {
			delete(g.topics, envelope.Topic)
		}
	}
	g.mu.Unlock()
	g.syncState()
	if g.state != nil {
		g.state.IncWSUnsubscribed()
	}
	_ = g.write(client.Session, map[string]any{"type": "ack", "id": envelope.ID, "topic": envelope.Topic})
}

func (g *Service) handlePublish(client *Client, envelope Envelope) {
	if envelope.Topic == "" && envelope.Target == "" {
		_ = g.write(client.Session, map[string]any{"type": "error", "id": envelope.ID, "error": "topic or target is required"})
		return
	}
	if envelope.Topic != "" && !g.allowTopic(client.AllowPublish, envelope.Topic) {
		_ = g.write(client.Session, map[string]any{"type": "error", "id": envelope.ID, "error": "publish not allowed"})
		return
	}
	resp, err := g.master.WsPublish(context.Background(), phpmaster.WsActionRequest{
		App:      client.App,
		ClientID: client.ID,
		Topic:    envelope.Topic,
		Target:   envelope.Target,
		Payload:  envelope.Payload,
		Meta:     envelope.Meta,
	})
	if err == nil && resp != nil && !resp.Allow {
		_ = g.write(client.Session, map[string]any{"type": "error", "id": envelope.ID, "error": "publish denied"})
		return
	}

	_, _ = g.master.WsMessage(context.Background(), phpmaster.WsMessageRequest{
		ID:        envelope.ID,
		Type:      envelope.Type,
		App:       client.App,
		ClientID:  client.ID,
		Topic:     envelope.Topic,
		Target:    envelope.Target,
		Payload:   envelope.Payload,
		Meta:      envelope.Meta,
		ReplyTo:   envelope.ReplyTo,
		Timestamp: envelope.Timestamp,
	})

	if envelope.Target != "" {
		_ = g.pushClient(envelope.Target, envelope)
	} else {
		g.publishToTopic(envelope.Topic, envelope)
	}
	if g.state != nil {
		g.state.IncWSPublished()
	}
	_ = g.write(client.Session, map[string]any{"type": "ack", "id": envelope.ID, "topic": envelope.Topic, "target": envelope.Target})
}

func (g *Service) publishToTopic(topic string, envelope Envelope) {
	g.mu.RLock()
	ids := make([]string, 0, len(g.topics[topic]))
	for id := range g.topics[topic] {
		ids = append(ids, id)
	}
	g.mu.RUnlock()
	for _, id := range ids {
		_ = g.pushClient(id, map[string]any{
			"type":      "event",
			"topic":     topic,
			"payload":   envelope.Payload,
			"meta":      envelope.Meta,
			"timestamp": envelope.Timestamp,
			"reply_to":  envelope.ID,
		})
	}
}

func (g *Service) pushClient(clientID string, payload any) error {
	g.mu.RLock()
	client := g.clients[clientID]
	g.mu.RUnlock()
	if client == nil {
		return errors.New("client not found")
	}
	return g.write(client.Session, payload)
}

func (g *Service) kick(clientID string) error {
	g.mu.RLock()
	client := g.clients[clientID]
	g.mu.RUnlock()
	if client == nil {
		return nil
	}
	if g.state != nil {
		g.state.IncWSKicked()
	}
	return client.Session.Close()
}

func (g *Service) findBySession(session *melody.Session) *Client {
	id, ok := session.Get("client_id")
	if !ok {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.clients[id.(string)]
}

func (g *Service) removeClient(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	client := g.clients[id]
	if client == nil {
		return
	}
	for topic := range client.Topics {
		if members, ok := g.topics[topic]; ok {
			delete(members, id)
			if len(members) == 0 {
				delete(g.topics, topic)
			}
		}
	}
	delete(g.clients, id)
}

func (g *Service) clientsSnapshot() []map[string]any {
	g.mu.RLock()
	defer g.mu.RUnlock()
	items := make([]map[string]any, 0, len(g.clients))
	for _, client := range g.clients {
		items = append(items, map[string]any{
			"client_id": client.ID,
			"app":       client.App,
			"type":      client.Type,
			"topics":    len(client.Topics),
			"meta":      client.Meta,
		})
	}
	return items
}

func (g *Service) topicsSnapshot() []map[string]any {
	g.mu.RLock()
	defer g.mu.RUnlock()
	items := make([]map[string]any, 0, len(g.topics))
	for topic, members := range g.topics {
		items = append(items, map[string]any{
			"topic":   topic,
			"clients": len(members),
		})
	}
	return items
}

func (g *Service) syncState() {
	if g.state == nil {
		return
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	g.state.SetWSOnline(len(g.clients))
	g.state.SetWSTopics(len(g.topics))
}

func (g *Service) write(session *melody.Session, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return session.Write(body)
}

func (g *Service) allowTopic(allow map[string][]string, topic string) bool {
	if len(allow) == 0 {
		return true
	}
	patterns := make([]string, 0)
	for _, items := range allow {
		patterns = append(patterns, items...)
	}
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if matchTopic(pattern, topic) {
			return true
		}
	}
	return false
}

func matchTopic(pattern string, topic string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == topic
	}
	prefix := strings.TrimSuffix(pattern, "*")
	return strings.HasPrefix(topic, prefix)
}

func nowUnix() int64 {
	return time.Now().Unix()
}
