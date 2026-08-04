//go:build darwin || linux

package cdp

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// testPeer is a minimal server-side frame reader/writer used to exercise
// WSConn without a real Chrome.
type testPeer struct {
	conn net.Conn
	r    *bufio.Reader
}

func newTestPeer(t *testing.T) (*testPeer, *WSConn) {
	t.Helper()
	// Real TCP loopback: kernel-buffered, so writes don't deadlock against
	// readers the way net.Pipe's synchronous channel does.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	type conns struct{ server, client net.Conn }
	ch := make(chan conns, 1)
	go func() {
		s, err := ln.Accept()
		if err != nil {
			ch <- conns{}
			return
		}
		ch <- conns{server: s}
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if got.server == nil {
		t.Fatal("accept failed")
	}
	ln.Close()
	peer := &testPeer{conn: got.server, r: bufio.NewReader(got.server)}
	ws := &WSConn{conn: client, r: bufio.NewReader(client)}
	t.Cleanup(func() { client.Close(); got.server.Close() })
	return peer, ws
}

// readClientFrame parses a client→server frame (must be masked).
func (p *testPeer) readClientFrame() (opcode byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(p.r, hdr[:]); err != nil {
		return
	}
	opcode = hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	ln := int(hdr[1] & 0x7F)
	if ln == 126 {
		var b [2]byte
		if _, err = io.ReadFull(p.r, b[:]); err != nil {
			return
		}
		ln = int(binary.BigEndian.Uint16(b[:]))
	} else if ln == 127 {
		var b [8]byte
		if _, err = io.ReadFull(p.r, b[:]); err != nil {
			return
		}
		ln = int(binary.BigEndian.Uint64(b[:]))
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(p.r, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, ln)
	if _, err = io.ReadFull(p.r, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return
}

// sendServerFrame writes an unmasked server→client frame.
func (p *testPeer) sendServerFrame(fin bool, opcode byte, payload []byte) error {
	hdr := []byte{opcode}
	if fin {
		hdr[0] |= 0x80
	}
	ln := len(payload)
	switch {
	case ln < 126:
		hdr = append(hdr, byte(ln))
	case ln < 65536:
		hdr = append(hdr, 126, byte(ln>>8), byte(ln))
	default:
		hdr = append(hdr, 127)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(ln))
		hdr = append(hdr, b[:]...)
	}
	_, err := p.conn.Write(append(hdr, payload...))
	return err
}

func TestSendTextMasking(t *testing.T) {
	peer, ws := newTestPeer(t)
	msg := []byte(`{"id":1,"method":"Runtime.evaluate"}`)
	// net.Pipe: Write blocks until the peer reads — consume concurrently.
	type frame struct {
		opcode  byte
		payload []byte
		err     error
	}
	ch := make(chan frame, 1)
	go func() {
		op, p, err := peer.readClientFrame()
		ch <- frame{op, p, err}
	}()
	if err := ws.SendText(msg); err != nil {
		t.Fatal(err)
	}
	f := <-ch
	if f.err != nil {
		t.Fatal(f.err)
	}
	if f.opcode != 0x1 {
		t.Fatalf("opcode = 0x%x, want 0x1", f.opcode)
	}
	if string(f.payload) != string(msg) {
		t.Fatalf("payload = %q, want %q", f.payload, msg)
	}
}

func TestReadMessage(t *testing.T) {
	peer, ws := newTestPeer(t)
	got := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		b, err := ws.ReadMessage()
		if err != nil {
			errCh <- err
			return
		}
		got <- b
	}()
	if err := peer.sendServerFrame(false, 0x1, []byte(`{"id":7,"result":{}`)); err != nil {
		t.Fatal(err)
	}
	// Fragment continuation.
	if err := peer.sendServerFrame(true, 0x0, []byte(`,"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	case b := <-got:
		want := `{"id":7,"result":{},"ok":true}`
		if string(b) != want {
			t.Fatalf("ReadMessage = %q, want %q", b, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadMessage timed out")
	}
}

func TestPingPong(t *testing.T) {
	peer, ws := newTestPeer(t)
	done := make(chan error, 1)
	go func() {
		_, err := ws.ReadMessage()
		done <- err
	}()
	if err := peer.sendServerFrame(true, 0x9, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	// Server should receive an automatic pong.
	opcode, payload, err := peer.readClientFrame()
	if err != nil {
		t.Fatal(err)
	}
	if opcode != 0xA {
		t.Fatalf("opcode = 0x%x, want 0xA (pong)", opcode)
	}
	if string(payload) != "ping" {
		t.Fatalf("pong payload = %q, want %q", payload, "ping")
	}
	// The read goroutine is still blocked; send a data frame so it exits.
	if err := peer.sendServerFrame(true, 0x1, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadMessage did not return after data frame")
	}
}

func TestReadClose(t *testing.T) {
	peer, ws := newTestPeer(t)
	// ws.Close() blocks writing the close frame until the peer reads it.
	consumed := make(chan struct{})
	go func() {
		peer.readClientFrame()
		close(consumed)
	}()
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	<-consumed
	// TCP returns io.EOF once the peer closed after the close frame.
	if _, err := ws.ReadMessage(); err != io.EOF {
		t.Fatalf("ReadMessage after close = %v, want io.EOF", err)
	}
}
