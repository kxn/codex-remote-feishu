package install

// Minimal HTTP/1.1 client used only by the upgrade-helper health probe.
//
// The upgrade shim links this package, and the probe is its only consumer of
// net/http. Polling a handful of local loopback endpoints does not justify
// pulling the full net/http + crypto/tls + crypto/x509 + x/net/http2 stack
// into the shim binary, so this implementation speaks just enough HTTP/1.1
// over a raw TCP connection:
//
//   - absolute http:// URLs only (the probe targets are always local loopback)
//   - GET with "Connection: close" so the body is delimited by EOF
//   - Content-Length and chunked transfer encoding are both handled
//
// Behavior (status codes, error strings, timeouts) mirrors the previous
// net/http-based probe so the upgrade state machine is unchanged.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// probeHTTPTimeout bounds a single probe request (dial + response body).
const probeHTTPTimeout = 5 * time.Second

func probeHTTPGet(ctx context.Context, rawURL string) (int, []byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid probe url %q: %w", rawURL, err)
	}
	if u.Scheme != "http" {
		return 0, nil, fmt.Errorf("probe url %q must use http scheme", rawURL)
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "80")
	}

	conn, err := (&net.Dialer{Timeout: probeHTTPTimeout}).DialContext(ctx, "tcp", host)
	if err != nil {
		return 0, nil, err
	}
	defer conn.Close()

	deadline := time.Now().Add(probeHTTPTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", u.RequestURI(), u.Host)
	if _, err := io.WriteString(conn, req); err != nil {
		return 0, nil, err
	}

	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil {
		return 0, nil, err
	}
	parts := strings.SplitN(strings.TrimSpace(statusLine), " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "HTTP/") {
		return 0, nil, fmt.Errorf("malformed http status line %q", strings.TrimSpace(statusLine))
	}
	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, nil, fmt.Errorf("malformed http status code %q", parts[1])
	}

	contentLength := -1
	chunked := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return 0, nil, err
		}
		if strings.TrimRight(line, "\r\n") == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "content-length":
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				contentLength = n
			}
		case "transfer-encoding":
			if strings.Contains(strings.ToLower(value), "chunked") {
				chunked = true
			}
		}
	}

	body, err := readProbeBody(br, chunked, contentLength)
	if err != nil {
		return 0, nil, err
	}
	return statusCode, body, nil
}

func readProbeBody(br *bufio.Reader, chunked bool, contentLength int) ([]byte, error) {
	switch {
	case chunked:
		var body []byte
		for {
			sizeLine, err := br.ReadString('\n')
			if err != nil {
				return nil, err
			}
			sizeStr := strings.TrimSpace(strings.SplitN(sizeLine, ";", 2)[0])
			size, err := strconv.ParseInt(sizeStr, 16, 64)
			if err != nil {
				return nil, fmt.Errorf("malformed chunk size %q", sizeStr)
			}
			if size == 0 {
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return nil, err
					}
					if strings.TrimRight(line, "\r\n") == "" {
						return body, nil
					}
				}
			}
			chunk := make([]byte, size)
			if _, err := io.ReadFull(br, chunk); err != nil {
				return nil, err
			}
			body = append(body, chunk...)
			if _, err := br.ReadString('\n'); err != nil { // trailing CRLF
				return nil, err
			}
		}
	case contentLength >= 0:
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(br, body); err != nil {
			return nil, err
		}
		return body, nil
	default:
		// No Content-Length and not chunked: with "Connection: close" the
		// body runs until EOF.
		return io.ReadAll(br)
	}
}

// expectHTTPStatus issues a probe GET and fails unless the endpoint responds
// with the wanted status code. want == 200 unless a future endpoint needs a
// different code; keep the caller explicit.
func expectHTTPStatus(ctx context.Context, rawURL string, want int) error {
	status, _, err := probeHTTPGet(ctx, rawURL)
	if err != nil {
		return err
	}
	if status != want {
		return fmt.Errorf("%s returned http %d", rawURL, status)
	}
	return nil
}

// fetchJSON issues a probe GET and decodes the response body as JSON.
func fetchJSON(ctx context.Context, rawURL string, out any) error {
	status, body, err := probeHTTPGet(ctx, rawURL)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("%s returned http %d", rawURL, status)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}
