package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Pin is a plex.tv PIN used for browser sign-in.
type Pin struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	AuthToken string `json:"authToken"`
}

// User is the signed-in plex.tv account.
type User struct {
	ID       int64  `json:"id"`
	UUID     string `json:"uuid"`
	Username string `json:"username"`
	Title    string `json:"title"`
	Email    string `json:"email"`
}

// CreatePin starts the sign-in flow.
func (c *Client) CreatePin(ctx context.Context) (*Pin, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "https://plex.tv/api/v2/pins?strong=true", "")
	if err != nil {
		return nil, err
	}
	var pin Pin
	if err := c.send(req, &pin); err != nil {
		return nil, err
	}
	return &pin, nil
}

// AuthURL is the page the user opens to approve the PIN.
func (c *Client) AuthURL(pin *Pin) string {
	v := url.Values{}
	v.Set("clientID", c.ClientID)
	v.Set("code", pin.Code)
	v.Set("context[device][product]", Product)
	return "https://app.plex.tv/auth#?" + v.Encode()
}

// WaitForToken polls the PIN until the user approves it or ctx ends.
func (c *Client) WaitForToken(ctx context.Context, pin *Pin) (string, error) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-t.C:
		}
		var p Pin
		err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("https://plex.tv/api/v2/pins/%d", pin.ID), "", &p)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", err
			}
			// 404 means the PIN expired; anything else is worth retrying.
			continue
		}
		if p.AuthToken != "" {
			return p.AuthToken, nil
		}
	}
}

// CurrentUser returns the account that owns token.
func (c *Client) CurrentUser(ctx context.Context, token string) (*User, error) {
	var u User
	if err := c.doJSON(ctx, http.MethodGet, "https://plex.tv/api/v2/user", token, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// HomeUser is a member of the account's Plex Home, including managed users.
type HomeUser struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Username string `json:"username"`
	Admin    bool   `json:"admin"`
}

// HomeUsers lists the Plex Home members. Accounts without a Plex Home get
// an error or an empty list.
func (c *Client) HomeUsers(ctx context.Context, token string) ([]HomeUser, error) {
	var raw json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, "https://clients.plex.tv/api/v2/home/users", token, &raw); err != nil {
		return nil, err
	}
	// The endpoint has returned both a bare array and {"users": [...]}.
	var users []HomeUser
	if err := json.Unmarshal(raw, &users); err == nil {
		return users, nil
	}
	var wrapped struct {
		Users []HomeUser `json:"users"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Users, nil
}
