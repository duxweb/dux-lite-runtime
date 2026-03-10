package gateway

import "encoding/json"

type Control struct {
	service *Service
}

type Empty struct{}

type PublishRequest struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
	Meta    json.RawMessage `json:"meta"`
}

type ListRequest struct {
	Scope string `json:"scope"`
}

type PushClientRequest struct {
	ClientID string          `json:"client_id"`
	Payload  json.RawMessage `json:"payload"`
	Meta     json.RawMessage `json:"meta"`
}

type KickRequest struct {
	ClientID string `json:"client_id"`
}

type ControlResponse struct {
	OK    bool             `json:"ok"`
	Items []map[string]any `json:"items,omitempty"`
}

func NewControl(service *Service) *Control {
	return &Control{service: service}
}

func (c *Control) Publish(request PublishRequest, reply *ControlResponse) error {
	payload := decodeRawMap(request.Payload)
	meta := decodeRawMap(request.Meta)
	c.service.publishToTopic(request.Topic, Envelope{
		Type:      "event",
		Topic:     request.Topic,
		Payload:   payload,
		Meta:      meta,
		Timestamp: nowUnix(),
	})
	reply.OK = true
	return nil
}

func (c *Control) PushClient(request PushClientRequest, reply *ControlResponse) error {
	payload := decodeRawMap(request.Payload)
	meta := decodeRawMap(request.Meta)
	reply.OK = c.service.pushClient(request.ClientID, map[string]any{
		"type":      "event",
		"payload":   payload,
		"meta":      meta,
		"timestamp": nowUnix(),
	}) == nil
	return nil
}

func (c *Control) Kick(request KickRequest, reply *ControlResponse) error {
	reply.OK = c.service.kick(request.ClientID) == nil
	return nil
}

func (c *Control) Clients(_ ListRequest, reply *ControlResponse) error {
	reply.OK = true
	reply.Items = c.service.clientsSnapshot()
	return nil
}

func (c *Control) Topics(_ ListRequest, reply *ControlResponse) error {
	reply.OK = true
	reply.Items = c.service.topicsSnapshot()
	return nil
}

func decodeRawMap(data json.RawMessage) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil || payload == nil {
		return map[string]any{}
	}
	return payload
}
