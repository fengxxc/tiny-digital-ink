package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	uuid "github.com/satori/go.uuid"
	"github.com/skip2/go-qrcode"
)

var (
	PORT = "20230"
)

func main() {
	cache := NewCache[string, *Session]()
	server(cache)
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Session struct {
	Token      string
	CustomerId string
	Client     *websocket.Conn
}

type PageData struct {
	Title string
	Token string
}

type WsResponse struct {
	Type WsResType   `json:"type"`
	Data interface{} `json:"data"`
}

func (wr *WsResponse) ToJsonString() string {
	json, _ := json.Marshal(wr)
	return string(json)
}

type WsResType int32

const (
	WsResType_Error WsResType = iota
	WsResType_Message
	WsResType_Image
)

func warpWsResponse(wsType WsResType, data interface{}) []byte {
	wr := &WsResponse{
		Type: wsType,
		Data: data,
	}
	res := wr.ToJsonString()
	return []byte(res)
}

func server(cache *Cache[string, *Session]) {
	http.HandleFunc("/index", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		customerId := q.Get("id")
		if customerId == "" {
			w.Write([]byte("'id' is required!"))
			return
		}
		newUUID := uuid.NewV4()
		session := &Session{
			Token:      newUUID.String(),
			CustomerId: customerId,
			Client:     nil,
		}
		cache.Set(newUUID.String(), session)
		data := &PageData{
			Title: "Hello, Please scan the QR code.",
			Token: newUUID.String(),
		}
		t, _ := template.ParseFiles("index.html")
		t.Execute(w, data)
	})

	http.HandleFunc("/qrcode", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		token := q.Get("token")
		if token == "" {
			// w.Write([]byte("'token' is required!"))
			return
		}
		scheme := q.Get("scheme")
		if scheme == "" {
			scheme = "http"
		}
		qr, err := qrcode.New(scheme+"://"+r.Host+strings.Replace(r.URL.Path, "/qrcode", "", 1)+"/ink?token="+token, qrcode.Medium)
		if err != nil {
			fmt.Println(err)
		}
		qrPng, _ := qr.PNG(256)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(qrPng)))
		w.Write(qrPng)
	})

	http.HandleFunc("/watch", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			fmt.Println("upgrade error:", err)
			return
		}
		defer conn.Close()

		token := r.URL.Query().Get("token")
		if token == "" {
			conn.WriteMessage(websocket.TextMessage, warpWsResponse(WsResType_Message, "token is required!"))
			return
		}

		session, ok := cache.Get(token)
		if !ok {
			conn.WriteMessage(websocket.TextMessage, warpWsResponse(WsResType_Message, "token is expired!"))
			return
		}
		session.Client = conn

		fmt.Println("Client connected")

		for {
			messageType, p, err := conn.ReadMessage()
			if err != nil {
				fmt.Println(err)
				return
			}

			fmt.Printf("Received message: %s\n", p)

			// Echo the message back to the client
			err = conn.WriteMessage(messageType, p)
			if err != nil {
				fmt.Println(err)
				return
			}
		}

	})

	http.HandleFunc("/ink", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		token := q.Get("token")

		if token == "" {
			w.Write([]byte("'token' is required!"))
			return
		}

		session, ok := cache.Get(token)
		if !ok {
			w.Write([]byte("token is expired!"))
			return
		}
		session.Client.WriteMessage(websocket.TextMessage, warpWsResponse(WsResType_Message, "Please draw on mobile."))

		t, _ := template.ParseFiles("ink.html")
		t.Execute(w, t)
	})

	http.HandleFunc("/ink-submit", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		token := q.Get("token")

		if token == "" {
			w.Write([]byte("'token' is required!"))
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
			return
		}

		// 解析Canvas数据
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		session, ok := cache.Get(token)
		if !ok {
			w.Write([]byte("token is expired!"))
			return
		}

		session.Client.WriteMessage(websocket.TextMessage, warpWsResponse(WsResType_Image, data))

	})

	fmt.Println("tiny-digital-ink start, listening: " + PORT + ".")
	http.ListenAndServe(":"+PORT, nil)
}

/**
 * Cache 一个简单的Key-Value缓存结构
 * 使用sync.RWMutex实现读写锁，以确保在多个goroutine并发访问缓存时的安全性
 */
type Cache[K comparable, V any] struct {
	data map[K]V
	mu   sync.RWMutex
}

// NewCache 创建一个新的缓存实例
func NewCache[K comparable, V any]() *Cache[K, V] {
	return &Cache[K, V]{
		data: make(map[K]V),
	}
}

// Set 将键值对存储到缓存中
func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = value
}

// Get 从缓存中获取指定键的值
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.data[key]
	return value, ok
}

// Delete 从缓存中删除指定键的值
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}
