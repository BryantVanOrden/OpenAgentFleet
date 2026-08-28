package httpapi

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 16 << 10,
	// Origin is not a security boundary here: the connection still has to carry
	// a valid token, and the Flutter app has no browser origin at all.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleEvents streams fleet events. Pass ?instance_id= to narrow the feed —
// which is what the mobile app does when it has one instance on screen, so a
// busy fleet does not drain a phone battery.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	c, err := s.parseToken(tokenFrom(r))
	if err != nil {
		fail(w, http.StatusUnauthorized, "authentication required")
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote the error
	}
	defer conn.Close()

	sub := s.bus.Subscribe(r.URL.Query().Get("instance_id"))
	defer sub.Close()

	s.log.Debug("event stream opened", "user", c.Email)

	const (
		pingEvery = 25 * time.Second
		pongWait  = 60 * time.Second
		writeWait = 10 * time.Second
	)

	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	// Reader goroutine: we do not expect client messages, but the read loop is
	// what surfaces a closed connection and keeps pong handling alive.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(pingEvery)
	defer ticker.Stop()

	for {
		select {
		case <-closed:
			return
		case <-r.Context().Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
