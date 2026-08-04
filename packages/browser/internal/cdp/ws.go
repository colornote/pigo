//go:build darwin || linux

// Package cdp implements a minimal Chrome DevTools Protocol client using
// only the Go standard library: a WebSocket client (RFC 6455) plus CDP
// commands over it. No third-party dependencies.
package cdp

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

// wsGUID is the fixed GUID from RFC 6455 used to derive the accept key.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WSConn is a minimal RFC 6455 WebSocket client connection.
type WSConn struct {
	conn net.Conn
	r    *bufio.Reader
}

// Dial performs the WebSocket opening handshake against rawURL.
// The caller must send a `Origin`-free request; Chrome's
// --remote-allow-origins=* flag is what permits external WS clients.
func Dial(rawURL string, timeout time.Duration) (*WSConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse ws url: %w", err)
	}
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", host, err)
	}

	// Handshake request.
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\n"+
		"Connection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, host, key)
	conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake read: %w", err)
	}
	if !strings.Contains(status, " 101 ") {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake failed: %s", strings.TrimSpace(status))
	}
	var accept string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("handshake headers: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-accept:") {
			accept = strings.TrimSpace(line[len("sec-websocket-accept:"):])
		}
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	expected := base64.StdEncoding.EncodeToString(sum[:])
	if accept != expected {
		conn.Close()
		return nil, errors.New("websocket accept key mismatch")
	}
	conn.SetDeadline(time.Time{})
	return &WSConn{conn: conn, r: br}, nil
}

// SendText sends a single masked text frame (FIN=1, opcode=1).
func (c *WSConn) SendText(payload []byte) error {
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header := []byte{0x81} // FIN + text opcode
	ln := len(payload)
	switch {
	case ln < 126:
		header = append(header, byte(0x80|ln))
	case ln < 65536:
		header = append(header, byte(0x80|126), byte(ln>>8), byte(ln))
	default:
		header = append(header, byte(0x80|127))
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(ln))
		header = append(header, b[:]...)
	}
	header = append(header, mask...)
	masked := make([]byte, ln)
	for i := 0; i < ln; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := c.conn.Write(append(header, masked...)); err != nil {
		return err
	}
	return nil
}

// ReadMessage reads one complete text message, transparently reassembling
// fragmented frames and answering pings with pongs. Returns io.EOF on a
// close frame or connection drop.
func (c *WSConn) ReadMessage() ([]byte, error) {
	var buf []byte
	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil, io.EOF
			}
			return nil, err
		}
		switch opcode {
		case 0x1, 0x2: // text, binary — start of a message
			buf = append(buf, payload...)
			if fin {
				return buf, nil
			}
		case 0x0: // continuation
			buf = append(buf, payload...)
			if fin {
				return buf, nil
			}
		case 0x9: // ping → pong
			if err := c.sendControl(0xA, payload); err != nil {
				return nil, err
			}
		case 0xA: // pong — ignore
		case 0x8: // close
			return nil, io.EOF
		default:
			return nil, fmt.Errorf("unexpected opcode 0x%x", opcode)
		}
	}
}

// Close sends a close frame and closes the underlying connection.
func (c *WSConn) Close() error {
	_ = c.sendControl(0x8, nil)
	return c.conn.Close()
}

// readFrame reads a single frame from the wire.
func (c *WSConn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(c.r, hdr[:]); err != nil {
		return
	}
	fin = hdr[0]&0x80 != 0
	opcode = hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	ln := int64(hdr[1] & 0x7F)
	switch ln {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(c.r, b[:]); err != nil {
			return
		}
		ln = int64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(c.r, b[:]); err != nil {
			return
		}
		ln = int64(binary.BigEndian.Uint64(b[:]))
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.r, mask[:]); err != nil {
			return
		}
	}
	if ln > 64<<20 { // 64 MiB safety cap
		err = errors.New("frame too large")
		return
	}
	payload = make([]byte, ln)
	if _, err = io.ReadFull(c.r, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return
}

// sendControl writes an unmasked control frame (close/ping/pong).
func (c *WSConn) sendControl(opcode byte, payload []byte) error {
	header := []byte{0x80 | opcode, byte(len(payload))}
	if _, err := c.conn.Write(append(header, payload...)); err != nil {
		return err
	}
	return nil
}
