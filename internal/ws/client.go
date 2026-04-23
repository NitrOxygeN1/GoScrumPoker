package ws

import (
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 8192
	sendBuffer     = 256
)

// Client wraps a single WebSocket connection for one room.
type Client struct {
	hub    *Hub
	roomID string
	conn   *websocket.Conn
	send   chan []byte
	logger *slog.Logger

	mu        sync.Mutex
	userID    string
	joined    bool
	closeOnce sync.Once
}

// NewClient constructs a client. Run readPump/writePump from separate goroutines.
func NewClient(
	hub *Hub,
	roomID string,
	conn *websocket.Conn,
	logger *slog.Logger,
) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		hub:    hub,
		roomID: roomID,
		conn:   conn,
		send:   make(chan []byte, sendBuffer),
		logger: logger,
	}
}

func (c *Client) enqueue(payload []byte) {
	select {
	case c.send <- payload:
	default:
		c.logger.Warn("websocket client slow; dropping outbound message", "room_id", c.roomID)
	}
}

func (c *Client) setJoined(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.userID = userID
	c.joined = true
}

func (c *Client) joinedUser() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userID, c.joined
}

func (c *Client) readPump(handle func([]byte) error) {
	defer c.cleanup()

	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	c.conn.SetReadLimit(maxMessageSize)

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Info("websocket read closed", "room_id", c.roomID, "err", err)
			}
			return
		}
		if err := handle(data); err != nil {
			c.logger.Debug("websocket handler error", "room_id", c.roomID, "err", err)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Run pumps for a connected client. Blocks until the connection ends.
func (c *Client) Run() {
	go c.writePump()
	c.readPump(c.dispatch)
}

func (c *Client) dispatch(raw []byte) error {
	var env ClientMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		c.enqueue(serverErrorBytes("invalid json"))
		return err
	}

	switch env.Type {
	case MsgJoin, MsgVote, MsgReveal, MsgReset:
		if err := c.hub.Submit(ClientEvent{
			Type:    env.Type,
			RoomID:  c.roomID,
			Client:  c,
			Payload: env.Payload,
		}); err != nil {
			c.enqueue(serverErrorBytes("server busy"))
			return err
		}
		return nil
	default:
		c.enqueue(serverErrorBytes("unknown message type"))
		return errors.New("unknown message type")
	}
}

func (c *Client) cleanup() {
	c.closeOnce.Do(func() {
		c.hub.Leave(c.roomID, c)
		close(c.send)
	})
}
