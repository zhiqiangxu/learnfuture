package handler

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"learn_future/internal/svc"
	"learn_future/internal/ws"
	"learn_future/pkg/jwt"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for WeChat Mini Program
	},
}

func WebSocketHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}

		claims, err := jwt.ParseToken(token, svcCtx.Config.JWT.Secret)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[WS] upgrade error: %v", err)
			return
		}

		client := ws.NewClient(svcCtx.Hub, conn, claims.UserID)
		svcCtx.Hub.Register(client)

		go client.WritePump()
		go client.ReadPump()
	}
}
