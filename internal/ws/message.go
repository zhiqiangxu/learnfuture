package ws

import "encoding/json"

type Message struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func NewMessage(msgType string, data interface{}) ([]byte, error) {
	d, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	msg := Message{Type: msgType, Data: d}
	return json.Marshal(msg)
}
