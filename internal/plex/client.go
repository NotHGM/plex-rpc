// Package plex talks to plex.tv and to Plex Media Server.
package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// Product is the name shown in the Plex "Authorized Devices" list.
const Product = "Plex RPC"

// ErrUnauthorized is returned when plex.tv or the server rejects the token.
var ErrUnauthorized = errors.New("plex: unauthorized")

// ErrNotFound is returned when an item or image does not exist.
var ErrNotFound = errors.New("plex: not found")

// Client holds the identity headers every Plex request needs.
type Client struct {
	ClientID string
	Version  string
	HTTP     *http.Client
}

// NewClient returns a Client identifying itself with clientID.
func NewClient(clientID, version string) *Client {
	return &Client{
		ClientID: clientID,
		Version:  version,
		HTTP:     &http.Client{Timeout: 10 * time.Second},
	}
}

func platformName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	default:
		return "Linux"
	}
}

func (c *Client) newRequest(ctx context.Context, method, url, token string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	h := req.Header
	h.Set("Accept", "application/json")
	h.Set("X-Plex-Product", Product)
	h.Set("X-Plex-Version", c.Version)
	h.Set("X-Plex-Client-Identifier", c.ClientID)
	h.Set("X-Plex-Platform", platformName())
	h.Set("X-Plex-Device", platformName())
	h.Set("X-Plex-Device-Name", host)
	if token != "" {
		h.Set("X-Plex-Token", token)
	}
	return req, nil
}

// doJSON performs a request and decodes a JSON body into out.
func (c *Client) doJSON(ctx context.Context, method, url, token string, out any) error {
	req, err := c.newRequest(ctx, method, url, token)
	if err != nil {
		return err
	}
	return c.send(req, out)
}

func (c *Client) send(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("plex: %s %s: %s: %s", req.Method, redact(req.URL.String()), resp.Status, body)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// redact keeps tokens out of error messages and logs.
func redact(u string) string {
	if i := strings.Index(u, "X-Plex-Token="); i >= 0 {
		return u[:i] + "X-Plex-Token=REDACTED"
	}
	return u
}
