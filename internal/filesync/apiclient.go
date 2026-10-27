package filesync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// maxResponse bounds what one answer from a provider may weigh.
const maxResponse = 64 << 20

// apiClient makes the read-only calls to one provider. It sends the access
// token only to the provider's own host (a "next page" link in an answer could
// name any), retries what a provider asks to be retried, and refreshes a token
// the provider turns down once.
type apiClient struct {
	hc     *http.Client
	tokens *TokenSource
	// host is the only host (with scheme) the token goes to.
	host  string
	sleep func(context.Context, time.Duration) error
	// pauses between attempts, shortened in tests.
	maxWait time.Duration
}

func newAPIClient(hc *http.Client, tokens *TokenSource, base string) (*apiClient, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("bad API address %q", base)
	}
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &apiClient{hc: hc, tokens: tokens, host: u.Scheme + "://" + u.Host, sleep: sleepCtx, maxWait: 60 * time.Second}, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sameHost reports an address on the provider's own host.
func (c *apiClient) sameHost(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme+"://"+u.Host == c.host
}

// get returns the body of a GET.
func (c *apiClient) get(ctx context.Context, rawURL string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, rawURL, nil)
}

// postJSON returns the body of a POST with a JSON body.
func (c *apiClient) postJSON(ctx context.Context, rawURL string, body any) ([]byte, error) {
	if body == nil { // a call with no arguments takes no body at all
		return c.do(ctx, http.MethodPost, rawURL, nil)
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, http.MethodPost, rawURL, b)
}

func (c *apiClient) do(ctx context.Context, method, rawURL string, body []byte) ([]byte, error) {
	if !c.sameHost(rawURL) {
		return nil, fmt.Errorf("refusing to send the sign-in to %s: it is not the provider's address", hostOf(rawURL))
	}
	refreshed, server5xx := false, 0
	for attempt := 0; ; attempt++ {
		token, err := c.tokens.Access(ctx)
		if err != nil {
			return nil, err
		}
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", hostOf(rawURL), err)
		}
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
		resp.Body.Close()
		if rerr != nil {
			return nil, rerr
		}
		if len(data) > maxResponse {
			return nil, errors.New("an answer from the provider was unreasonably large")
		}
		switch {
		case resp.StatusCode < 300:
			return data, nil
		case resp.StatusCode == http.StatusUnauthorized && !refreshed:
			refreshed = true // the token may just have been revoked early: one fresh one
			c.tokens.Invalidate()
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, &AuthError{Msg: "the provider no longer accepts the saved sign-in: connect the source again"}
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable || isRateLimit(resp.StatusCode, data):
			if attempt >= 8 {
				return nil, errors.New("the provider keeps asking to slow down: try again later")
			}
			if err := c.sleep(ctx, c.retryAfter(resp, attempt)); err != nil {
				return nil, err
			}
		case resp.StatusCode >= 500:
			server5xx++
			if server5xx > 3 {
				return nil, apiError(resp.StatusCode, data)
			}
			if err := c.sleep(ctx, c.retryAfter(resp, attempt)); err != nil {
				return nil, err
			}
		default:
			return nil, apiError(resp.StatusCode, data)
		}
	}
}

// isRateLimit reports Google's way of saying "slow down": a 403 whose reason
// is a rate limit, not a refusal.
func isRateLimit(status int, body []byte) bool {
	return status == http.StatusForbidden &&
		(bytes.Contains(body, []byte("rateLimitExceeded")) || bytes.Contains(body, []byte("userRateLimitExceeded")))
}

// retryAfter is the wait a provider asked for, else an exponential one.
func (c *apiClient) retryAfter(resp *http.Response, attempt int) time.Duration {
	d := time.Duration(1<<min(attempt, 6)) * time.Second
	if s := resp.Header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 0 {
			d = time.Duration(n) * time.Second
		}
	}
	return min(d, c.maxWait)
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return "the provider"
}

// apiError describes a refusal in one line, from the error text each provider
// puts in its answer (Graph, Google and Dropbox all use a JSON body).
func apiError(status int, body []byte) error {
	msg := ""
	var v struct {
		Error        json.RawMessage `json:"error"`
		ErrorSummary string          `json:"error_summary"`
	}
	if json.Unmarshal(body, &v) == nil {
		var obj struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		var s string
		switch {
		case v.ErrorSummary != "":
			msg = v.ErrorSummary
		case json.Unmarshal(v.Error, &obj) == nil && (obj.Message != "" || obj.Code != ""):
			msg = strings.TrimSpace(obj.Code + " " + obj.Message)
		case json.Unmarshal(v.Error, &s) == nil:
			msg = s
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, msg)
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return fmt.Errorf("the provider answered %d: %s", status, msg)
}
