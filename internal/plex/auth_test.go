package plex

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type rewrite struct{ target string }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	u.Scheme, u.Host = "http", strings.TrimPrefix(r.target, "http://")
	req2 := req.Clone(req.Context())
	req2.URL = &u
	return http.DefaultTransport.RoundTrip(req2)
}

func TestHomeUsersBothShapes(t *testing.T) {
	for name, body := range map[string]string{
		"array":   `[{"id":1,"title":"Main","admin":true},{"id":7,"title":"Kids"}]`,
		"wrapped": `{"id":99,"users":[{"id":1,"title":"Main","admin":true},{"id":7,"title":"Kids"}]}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v2/home/users" || r.Header.Get("X-Plex-Token") != "tok" {
				http.NotFound(w, r)
				return
			}
			io.WriteString(w, body)
		}))
		c := NewClient("id", "test")
		c.HTTP.Transport = rewrite{srv.URL}
		users, err := c.HomeUsers(context.Background(), "tok")
		srv.Close()
		if err != nil || len(users) != 2 || users[1].ID != 7 || users[1].Title != "Kids" || !users[0].Admin {
			t.Errorf("%s: %+v, %v", name, users, err)
		}
	}
}
