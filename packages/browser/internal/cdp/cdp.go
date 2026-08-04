//go:build darwin || linux

// Package cdp: Chrome DevTools Protocol client over the stdlib WebSocket.
package cdp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DefaultPort matches the topromax-ops Chrome baseline (AGENT.md §11.1.1).
const DefaultPort = 9222

// Tab is a Chrome tab as reported by /json/list.
type Tab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	WSURL string `json:"webSocketDebuggerUrl"`
}

// ListTabs returns all open tabs via GET /json/list.
func ListTabs(port int) ([]Tab, error) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return nil, fmt.Errorf("list tabs: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("list tabs: HTTP %d: %s", resp.StatusCode, body)
	}
	var tabs []Tab
	if err := json.Unmarshal(body, &tabs); err != nil {
		return nil, fmt.Errorf("list tabs: parse: %w", err)
	}
	return tabs, nil
}

// NewTab opens a new tab (PUT /json/new?url) and returns it.
func NewTab(port int, target string) (*Tab, error) {
	u := fmt.Sprintf("http://127.0.0.1:%d/json/new?%s", port, url.QueryEscape(target))
	req, err := http.NewRequest("PUT", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("new tab: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("new tab: HTTP %d: %s", resp.StatusCode, body)
	}
	var tab Tab
	if err := json.Unmarshal(body, &tab); err != nil {
		return nil, fmt.Errorf("new tab: parse: %w", err)
	}
	return &tab, nil
}

// CloseTab closes a tab via GET /json/close/<id>.
func CloseTab(port int, id string) error {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/close/%s", port, url.PathEscape(id)))
	if err != nil {
		return fmt.Errorf("close tab: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("close tab: HTTP %d: %s", resp.StatusCode, body)
	}
	return nil
}

// cdpResponse is what the read loop delivers for a command reply.
type cdpResponse struct {
	result json.RawMessage
	err    error
}

// Client is a CDP session attached to one tab's webSocketDebuggerUrl.
type Client struct {
	ws      *WSConn
	mu      sync.Mutex
	nextID  int
	pending map[int]chan cdpResponse
	closed  chan struct{}
	done    bool
}

// Connect attaches to a tab's WebSocket debugger URL.
func Connect(wsURL string) (*Client, error) {
	ws, err := Dial(wsURL, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("cdp connect: %w", err)
	}
	c := &Client{
		ws:      ws,
		pending: make(map[int]chan cdpResponse),
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// readLoop dispatches responses by id and wakes pending callers on drop.
func (c *Client) readLoop() {
	for {
		msg, err := c.ws.ReadMessage()
		if err != nil {
			c.failAll(err)
			return
		}
		var env struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(msg, &env); err != nil || env.ID == 0 {
			continue // events (Page.*, Runtime.*) are ignored
		}
		c.mu.Lock()
		ch, ok := c.pending[env.ID]
		if ok {
			delete(c.pending, env.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- cdpResponse{result: msg}
		}
	}
}

func (c *Client) failAll(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	c.done = true
	for id, ch := range c.pending {
		select {
		case ch <- cdpResponse{err: err}:
		default:
		}
		delete(c.pending, id)
	}
	close(c.closed)
}

// Send issues a CDP command and returns the raw response envelope.
func (c *Client) Send(method string, params map[string]interface{}, timeout time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return nil, io.EOF
	}
	c.nextID++
	id := c.nextID
	ch := make(chan cdpResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := map[string]interface{}{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	if err := c.ws.SendText(payload); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.err != nil {
			return nil, resp.err
		}
		var env struct {
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(resp.result, &env); err != nil {
			return nil, err
		}
		if env.Error != nil {
			return nil, fmt.Errorf("cdp %s: %s", method, env.Error.Message)
		}
		return resp.result, nil
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("cdp %s: timeout after %s", method, timeout)
	case <-c.closed:
		return nil, io.EOF
	}
}

// Evaluate runs a JS expression in the page and returns the serialized value
// (returnByValue + awaitPromise). A JS exception becomes a Go error.
func (c *Client) Evaluate(expr string) (interface{}, error) {
	raw, err := c.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
	}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	// Send returns the full message ({"id":…,"result":{…}}), so the
	// value lives two levels down: result.result.{type,value}.
	var env struct {
		Result struct {
			Result struct {
				Type  string      `json:"type"`
				Value interface{} `json:"value"`
			} `json:"result"`
			ExceptionDetails *struct {
				Text      string `json:"text"`
				Exception *struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if env.Result.ExceptionDetails != nil {
		msg := env.Result.ExceptionDetails.Text
		if env.Result.ExceptionDetails.Exception != nil && env.Result.ExceptionDetails.Exception.Description != "" {
			msg = env.Result.ExceptionDetails.Exception.Description
		}
		return nil, fmt.Errorf("js error: %s", msg)
	}
	if env.Result.Result.Type == "undefined" {
		return nil, nil
	}
	return env.Result.Result.Value, nil
}

// Screenshot captures the visible page as PNG bytes.
func (c *Client) Screenshot() ([]byte, error) {
	raw, err := c.Send("Page.captureScreenshot", map[string]interface{}{
		"format": "png",
	}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var env struct {
		Result struct {
			Data string `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(env.Result.Data)
}

// Close releases the pending callers and the underlying socket.
func (c *Client) Close() error {
	c.failAll(io.EOF)
	return c.ws.Close()
}
