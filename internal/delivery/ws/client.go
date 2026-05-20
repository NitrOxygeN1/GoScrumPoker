package ws

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// Heartbeat sizing: we want a Meet participant who drops off the call (network
// loss, device sleep, Meet keeping the iframe alive on its "you left the
// meeting" screen) to disappear from other clients' participant lists in
// roughly half a minute, not a full minute. pingPeriod is comfortably under a
// third of pongWait, so two consecutive missed pings still time out before
// the deadline — robust against a single dropped ping. Going much lower
// risks false-positive evictions when a backgrounded mobile tab is briefly
// throttled by the browser.
const (
	writeWait      = 10 * time.Second
	pongWait       = 30 * time.Second
	pingPeriod     = 10 * time.Second
	maxMessageSize = 8192
	sendBuffer     = 256
)

// Client wraps a single WebSocket connection for one room.
type Client struct {
	hub    *Hub
	roomID string
	conn   *websocket.Conn
	send   chan []byte
	logger zerolog.Logger

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
	logger zerolog.Logger,
) *Client {
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
		c.logger.Warn().Msg("websocket client slow; dropping outbound message")
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
				c.logger.Info().Err(err).Msg("websocket read closed")
			}
			return
		}
		if err := handle(data); err != nil {
			c.logger.Debug().Err(err).Msg("websocket handler error")
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
				c.logger.Debug().Err(err).Msg("websocket write failed")
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.logger.Debug().Err(err).Msg("websocket ping failed")
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
		c.logger.Warn().Err(err).Msg("websocket invalid json")
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
			c.logger.Error().Err(err).Str("ws_event", env.Type).Msg("websocket hub overloaded")
			c.enqueue(serverErrorBytes("server busy"))
			return err
		}
		return nil
	default:
		c.logger.Warn().Str("ws_event", env.Type).Msg("websocket unknown message type")
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

// CloseConn closes the WebSocket from the server side (e.g. graceful shutdown).
func (c *Client) CloseConn() {
	_ = c.conn.Close()
}
