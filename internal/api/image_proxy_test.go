package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestImageProxyGuards(t *testing.T) {
	s := newTestStore(t)
	user := newTestUser(t, s)
	e := echo.New()
	RegisterImageProxyRoutes(e, s, authMiddlewareFor(user))

	// A local image server: loopback, so it must be refused.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes())
	}))
	defer upstream.Close()

	for target, want := range map[string]int{
		upstream.URL + "/a.png": http.StatusForbidden,
		"file:///etc/passwd":    http.StatusBadRequest,
		"":                      http.StatusBadRequest,
	} {
		rec := performRequest(t, e, http.MethodGet, "/api/v1/image-proxy?url="+url.QueryEscape(target), nil, nil)
		if rec.Code != want {
			t.Errorf("proxy %q = %d, want %d", target, rec.Code, want)
		}
	}

	// The user's own Chevereto host may be private.
	if err := s.SetSetting(user.ID, "chevereto.domain", upstream.URL, false); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	rec := performRequest(t, e, http.MethodGet, "/api/v1/image-proxy?url="+url.QueryEscape(upstream.URL+"/a.png"), nil, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("trusted host proxy = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestPublicIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fd00::1": false, "fe80::1": false,
	} {
		if got := publicIP(net.ParseIP(ip)); got != want {
			t.Errorf("publicIP(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestImageProxyClientRedirects(t *testing.T) {
	client := imageProxyClient("")
	req := func(raw string) *http.Request {
		u, _ := url.Parse(raw)
		return &http.Request{URL: u}
	}
	if err := client.CheckRedirect(req("https://cdn.example.com/a.png"), nil); err != nil {
		t.Fatalf("https redirect refused: %v", err)
	}
	if err := client.CheckRedirect(req("file:///etc/passwd"), nil); err == nil {
		t.Fatal("redirect to file:// should be refused")
	}
	via := []*http.Request{req("https://a"), req("https://b"), req("https://c")}
	if err := client.CheckRedirect(req("https://d"), via); err == nil {
		t.Fatal("too many redirects should be refused")
	}
	// A redirect from a public host to a private address is refused at dial time.
	if _, err := client.Get("http://127.0.0.1:1/a.png"); err == nil {
		t.Fatal("dialling loopback should fail")
	}
}
