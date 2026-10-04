// Package discord is a minimal Discord IPC client for Rich Presence.
//
// It speaks the local RPC protocol directly so it can set the activity type
// (Watching / Listening), which most Go libraries do not expose.
package discord

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
	opPing      = 3
	opPong      = 4
)

// ErrNotRunning means no Discord client is listening on this machine.
var ErrNotRunning = errors.New("discord: client not running")

// Client is a connection to the local Discord app. It is safe for concurrent
// use and reconnects on the next call after an error.
type Client struct {
	clientID string

	mu   sync.Mutex
	conn net.Conn
	user string
}

// New returns a client for the given application ID.
func New(clientID string) *Client { return &Client{clientID: clientID} }

// ClientID returns the application ID in use.
func (c *Client) ClientID() string { return c.clientID }

// Connected reports whether a connection is open.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// User returns the Discord username of the connected client, if known.
func (c *Client) User() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.user
}

// Close drops the connection; Discord clears the activity on its own.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
}

func (c *Client) closeLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.user = ""
	}
}

func (c *Client) connectLocked() error {
	if c.conn != nil {
		return nil
	}
	if c.clientID == "" {
		return errors.New("discord: no application client ID configured")
	}
	var lastErr error = ErrNotRunning
	for i := 0; i < 10; i++ {
		conn, err := dial(i)
		if err != nil {
			continue
		}
		if err := c.handshake(conn); err != nil {
			_ = conn.Close()
			lastErr = err
			continue
		}
		c.conn = conn
		return nil
	}
	return lastErr
}

func (c *Client) handshake(conn net.Conn) error {
	if err := writeFrame(conn, opHandshake, map[string]any{"v": 1, "client_id": c.clientID}); err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetDeadline(time.Time{})
	op, body, err := readFrame(conn)
	if err != nil {
		return err
	}
	if op == opClose {
		return fmt.Errorf("discord: handshake rejected: %s", body)
	}
	var ready struct {
		Evt  string `json:"evt"`
		Data struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ready); err != nil {
		return err
	}
	if ready.Evt != "READY" {
		return fmt.Errorf("discord: unexpected handshake reply: %s", body)
	}
	c.user = ready.Data.User.Username
	return nil
}

// SetActivity shows a, or clears the presence when a is nil.
func (c *Client) SetActivity(a *Activity) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.connectLocked(); err != nil {
		return err
	}
	args := map[string]any{"pid": os.Getpid()}
	if a != nil {
		args["activity"] = a
	}
	err := c.command("SET_ACTIVITY", args)
	if err != nil {
		var rpcErr *RPCError
		if !errors.As(err, &rpcErr) {
			// Transport problem: reconnect next time.
			c.closeLocked()
		}
	}
	return err
}

// RPCError is an error reported by Discord for a command.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("discord: %s (code %d)", e.Message, e.Code) }

func (c *Client) command(cmd string, args any) error {
	nonce := newNonce()
	if err := writeFrame(c.conn, opFrame, map[string]any{"cmd": cmd, "args": args, "nonce": nonce}); err != nil {
		return err
	}
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer c.conn.SetDeadline(time.Time{})
	for {
		op, body, err := readFrame(c.conn)
		if err != nil {
			return err
		}
		switch op {
		case opClose:
			return fmt.Errorf("discord: connection closed: %s", body)
		case opPing:
			if err := writeRaw(c.conn, opPong, body); err != nil {
				return err
			}
			continue
		case opFrame:
		default:
			continue
		}
		var resp struct {
			Evt   string          `json:"evt"`
			Nonce string          `json:"nonce"`
			Data  json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return err
		}
		if resp.Nonce != nonce {
			continue // an event or a reply to something else
		}
		if resp.Evt == "ERROR" {
			e := &RPCError{}
			_ = json.Unmarshal(resp.Data, e)
			return e
		}
		return nil
	}
}

func newNonce() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeFrame(w io.Writer, op uint32, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeRaw(w, op, body)
}

func writeRaw(w io.Writer, op uint32, body []byte) error {
	var buf bytes.Buffer
	buf.Grow(8 + len(body))
	_ = binary.Write(&buf, binary.LittleEndian, op)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(body)))
	buf.Write(body)
	_, err := w.Write(buf.Bytes())
	return err
}

func readFrame(r io.Reader) (uint32, []byte, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	op := binary.LittleEndian.Uint32(hdr[0:4])
	n := binary.LittleEndian.Uint32(hdr[4:8])
	if n > 1<<20 {
		return 0, nil, fmt.Errorf("discord: frame too large (%d bytes)", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return op, body, nil
}
