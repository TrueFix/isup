package wss

import (
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

func connect(u url.URL) (*websocket.Conn, error) {
	// Set the timeout for the connection
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	// Attempt to dial the WebSocket server
	c, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return nil, err
	}
	return c, nil
}
