package ws

import (
	"sync"
)

type Hub struct {
	clients    sync.Map // userID -> *Client
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			// Close existing connection for same user
			if old, loaded := h.clients.LoadAndDelete(client.userID); loaded {
				oldClient := old.(*Client)
				close(oldClient.send)
			}
			h.clients.Store(client.userID, client)

		case client := <-h.unregister:
			if actual, ok := h.clients.Load(client.userID); ok {
				if actual.(*Client) == client {
					h.clients.Delete(client.userID)
					close(client.send)
				}
			}

		case message := <-h.broadcast:
			h.clients.Range(func(key, value interface{}) bool {
				client := value.(*Client)
				select {
				case client.send <- message:
				default:
					h.clients.Delete(key)
					close(client.send)
				}
				return true
			})
		}
	}
}

func (h *Hub) Register(c *Client) {
	h.register <- c
}

func (h *Hub) Unregister(c *Client) {
	h.unregister <- c
}

func (h *Hub) Broadcast(message []byte) {
	h.broadcast <- message
}

func (h *Hub) SendToUser(userID int64, message []byte) {
	if val, ok := h.clients.Load(userID); ok {
		client := val.(*Client)
		select {
		case client.send <- message:
		default:
		}
	}
}
