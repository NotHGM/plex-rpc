package plex

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Server is a reachable Plex Media Server.
type Server struct {
	Name      string
	MachineID string
	URI       string
	Token     string
	Owned     bool
}

type resource struct {
	Name             string       `json:"name"`
	Provides         string       `json:"provides"`
	ClientIdentifier string       `json:"clientIdentifier"`
	Owned            bool         `json:"owned"`
	AccessToken      string       `json:"accessToken"`
	Presence         bool         `json:"presence"`
	Connections      []connection `json:"connections"`
}

type connection struct {
	URI   string `json:"uri"`
	Local bool   `json:"local"`
	Relay bool   `json:"relay"`
}

// OwnedServers lists the servers owned by the account and picks a working
// connection for each one, preferring local, direct connections.
func (c *Client) OwnedServers(ctx context.Context, token string) ([]Server, error) {
	var res []resource
	url := "https://clients.plex.tv/api/v2/resources?includeHttps=1&includeRelay=1"
	if err := c.doJSON(ctx, http.MethodGet, url, token, &res); err != nil {
		return nil, err
	}
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		servers []Server
	)
	for _, r := range res {
		if !r.Owned || !slices.Contains(strings.Split(r.Provides, ","), "server") {
			continue
		}
		wg.Add(1)
		go func(r resource) {
			defer wg.Done()
			tok := r.AccessToken
			if tok == "" {
				tok = token
			}
			uri, err := c.pickConnection(ctx, r.Connections, tok)
			if err != nil {
				return
			}
			mu.Lock()
			servers = append(servers, Server{Name: r.Name, MachineID: r.ClientIdentifier, URI: uri, Token: tok, Owned: true})
			mu.Unlock()
		}(r)
	}
	wg.Wait()
	slices.SortFunc(servers, func(a, b Server) int { return strings.Compare(a.Name, b.Name) })
	if len(servers) == 0 {
		return nil, errors.New("no reachable Plex Media Server owned by this account")
	}
	return servers, nil
}

// pickConnection probes every connection at once and returns the best one
// that answered: local before remote, direct before relay.
func (c *Client) pickConnection(ctx context.Context, conns []connection, token string) (string, error) {
	rank := func(cn connection) int {
		r := 0
		if !cn.Local {
			r += 2
		}
		if cn.Relay {
			r += 4
		}
		if strings.HasPrefix(cn.URI, "http://") {
			r++ // prefer https when both work
		}
		return r
	}
	slices.SortStableFunc(conns, func(a, b connection) int { return rank(a) - rank(b) })

	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ok := make([]bool, len(conns))
	var wg sync.WaitGroup
	for i, cn := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok[i] = c.Ping(ctx, cn.URI, token) == nil
		}()
	}
	wg.Wait()
	for i, cn := range conns {
		if ok[i] {
			return cn.URI, nil
		}
	}
	return "", errors.New("no connection answered")
}

// Ping checks that a server answers with the given token.
func (c *Client) Ping(ctx context.Context, uri, token string) error {
	return c.doJSON(ctx, http.MethodGet, strings.TrimRight(uri, "/")+"/identity", token, nil)
}

// ServerIdentity reads the name and machine ID of a manually configured server.
func (c *Client) ServerIdentity(ctx context.Context, uri, token string) (Server, error) {
	var out struct {
		MediaContainer struct {
			MachineIdentifier string `json:"machineIdentifier"`
		} `json:"MediaContainer"`
	}
	uri = strings.TrimRight(uri, "/")
	if err := c.doJSON(ctx, http.MethodGet, uri+"/identity", token, &out); err != nil {
		return Server{}, err
	}
	var root struct {
		MediaContainer struct {
			FriendlyName string `json:"friendlyName"`
		} `json:"MediaContainer"`
	}
	_ = c.doJSON(ctx, http.MethodGet, uri+"/", token, &root)
	name := root.MediaContainer.FriendlyName
	if name == "" {
		name = uri
	}
	return Server{Name: name, MachineID: out.MediaContainer.MachineIdentifier, URI: uri, Token: token, Owned: true}, nil
}
