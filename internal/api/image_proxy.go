package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/songtianlun/diarum/internal/auth"
	"github.com/songtianlun/diarum/internal/config"
	"github.com/songtianlun/diarum/internal/store"
)

const (
	imageProxyMaxBytes = 25 << 20
	imageProxyTimeout  = 20 * time.Second
)

var errImageProxyBlocked = errors.New("destination not allowed")

// RegisterImageProxyRoutes lets the signed-in user fetch an external image
// through the server. Exporting an entry as a picture has to read every image
// in it, and image hosts (such as Chevereto CDNs) often send no CORS headers,
// which makes the browser refuse. Only images are relayed, and only from
// public addresses, except the user's own Chevereto host, which may live on
// a private network.
func RegisterImageProxyRoutes(e *echo.Echo, s *store.Store, authMiddleware echo.MiddlewareFunc) {
	configService := config.NewConfigService(s)
	e.GET("/api/v1/image-proxy", func(c echo.Context) error {
		target, err := url.Parse(strings.TrimSpace(c.QueryParam("url")))
		if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" {
			return badRequest("A http(s) image URL is required", nil)
		}
		trustedHost := ""
		if domain, _ := configService.GetString(auth.CurrentUser(c).ID, "chevereto.domain"); domain != "" {
			if !strings.Contains(domain, "://") {
				domain = "https://" + domain
			}
			if parsed, err := url.Parse(domain); err == nil {
				trustedHost = strings.ToLower(parsed.Hostname())
			}
		}

		ctx, cancel := context.WithTimeout(c.Request().Context(), imageProxyTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return badRequest("Invalid image URL", err)
		}
		req.Header.Set("Accept", "image/*")
		resp, err := imageProxyClient(trustedHost).Do(req)
		if err != nil {
			if errors.Is(err, errImageProxyBlocked) {
				return forbidden("Image host not allowed")
			}
			return echo.NewHTTPError(http.StatusBadGateway, "Failed to fetch image")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("Image host answered %d", resp.StatusCode))
		}
		mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if !strings.HasPrefix(mediaType, "image/") {
			return echo.NewHTTPError(http.StatusBadGateway, "Not an image")
		}
		if resp.ContentLength > imageProxyMaxBytes {
			return echo.NewHTTPError(http.StatusBadGateway, "Image too large")
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, imageProxyMaxBytes+1))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadGateway, "Failed to read image")
		}
		if len(body) > imageProxyMaxBytes {
			return echo.NewHTTPError(http.StatusBadGateway, "Image too large")
		}
		header := c.Response().Header()
		header.Set("Cache-Control", "private, max-age=86400")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Content-Length", strconv.Itoa(len(body)))
		return c.Blob(http.StatusOK, mediaType, body)
	}, authMiddleware)
}

// imageProxyClient refuses to connect to private, loopback and other
// non-public addresses, checked on the resolved IP of every connection (so
// redirects and DNS tricks are covered too), except for trustedHost.
func imageProxyClient(trustedHost string) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			d := *dialer
			if trustedHost == "" || !strings.EqualFold(host, trustedHost) {
				d.Control = func(_, address string, _ syscall.RawConn) error {
					ipText, _, err := net.SplitHostPort(address)
					if err != nil {
						return errImageProxyBlocked
					}
					if ip := net.ParseIP(ipText); ip == nil || !publicIP(ip) {
						return errImageProxyBlocked
					}
					return nil
				}
			}
			return d.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errImageProxyBlocked
			}
			return nil
		},
	}
}

func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		// 100.64.0.0/10 carrier-grade NAT and 0.0.0.0/8.
		(ip.To4() != nil && ((ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64) || ip.To4()[0] == 0)))
}
