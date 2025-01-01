package wss

import (
	"net/url"

	"github.com/gorilla/websocket"
)

type WSS struct {
	conn *websocket.Conn
}

func (w *WSS) Close() {
	if w.conn != nil {
		w.conn.Close()
	}
}

func (w *WSS) StartWorker(done chan struct{}) {
	Worker(w.conn, done)
}

func NewWSS(u url.URL) (*WSS, error) {
	c, err := connect(u)
	if err != nil {
		return nil, err
	}
	return &WSS{conn: c}, nil
}
