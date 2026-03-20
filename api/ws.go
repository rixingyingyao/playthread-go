package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rixingyingyao/playthread-go/models"
	"github.com/rs/zerolog/log"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingInterval   = 30 * time.Second
	maxMessageSize = 512
)

// WSClient 单个 WebSocket 连接
type WSClient struct {
	hub  *WSHub
	conn *websocket.Conn
	send chan []byte
}

// WSHub 管理所有 WebSocket 连接，实现 core.Subscriber 接口
type WSHub struct {
	mu         sync.RWMutex
	clients    map[*WSClient]struct{}
	broadcast  chan []byte
	register   chan *WSClient
	unregister chan *WSClient
}

// NewWSHub 创建 WebSocket 中心
func NewWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[*WSClient]struct{}),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *WSClient, 16),
		unregister: make(chan *WSClient, 16),
	}
}

// OnBroadcast 实现 core.Subscriber 接口，将事件转发给所有 WebSocket 客户端
func (h *WSHub) OnBroadcast(event models.BroadcastEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Warn().Err(err).Str("type", string(event.Type)).Msg("WS 事件序列化失败")
		return
	}
	select {
	case h.broadcast <- data:
	default:
		log.Warn().Str("type", string(event.Type)).Msg("WS 广播 channel 满，事件丢弃")
	}
}

// Run 启动 Hub 事件循环 + 心跳
func (h *WSHub) Run(ctx context.Context) {
	heartbeatTicker := time.NewTicker(5 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = struct{}{}
			h.mu.Unlock()
			log.Info().Int("total", h.ClientCount()).Msg("WS 客户端连接")

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Info().Int("total", h.ClientCount()).Msg("WS 客户端断开")

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					go h.removeClient(client)
				}
			}
			h.mu.RUnlock()

		case <-heartbeatTicker.C:
			evt := models.NewBroadcastEvent(models.EventHeartbeat, map[string]interface{}{
				"clients": h.ClientCount(),
			})
			data, _ := json.Marshal(evt)
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- data:
				default:
				}
			}
			h.mu.RUnlock()

		case <-ctx.Done():
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				delete(h.clients, client)
			}
			h.mu.Unlock()
			log.Info().Msg("WS Hub 已关闭")
			return
		}
	}
}

func (h *WSHub) removeClient(c *WSClient) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

// ClientCount 当前连接数
func (h *WSHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ServeWS 处理 WebSocket 升级请求
func (h *WSHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("WS 升级失败")
		return
	}

	client := &WSClient{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 256),
	}
	h.register <- client

	go client.writePump()
	go client.readPump()
}

// readPump 读取客户端消息（主要用于 ping/pong 保活）
func (c *WSClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Debug().Err(err).Msg("WS 读取异常")
			}
			return
		}
	}
}

// writePump 向客户端写入消息
func (c *WSClient) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
