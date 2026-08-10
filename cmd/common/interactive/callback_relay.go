package interactive

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"
)

const relayTimeout = 10 * time.Second

// relayCallback hands a callback URL the user copied out of their browser to the
// `auth login` waiting on this machine, by requesting it locally: the URL names
// the loopback listener that login bound, so no shared state is needed to find it.
func relayCallback(ctx context.Context, logger log.Logger, raw string) error {
	target, err := parseLoopbackCallback(raw)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return fmt.Errorf("build callback request: %w", err)
	}

	client := &http.Client{Timeout: relayTimeout}
	resp, err := client.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return fmt.Errorf("nothing is listening on %s — the sign-in it belongs to has stopped waiting (they expire 5 minutes after the URL is printed); start a new `auth login --print-url`", target.Host)
		}

		return fmt.Errorf("deliver the callback: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the waiting sign-in rejected this callback (HTTP %d) — check you copied the whole URL, and that it came from the sign-in still running", resp.StatusCode)
	}

	logger.TInfof("✅ Delivered the sign-in to the `auth login` waiting on %s.", target.Host)

	return nil
}

// parseLoopbackCallback accepts only what `auth login` itself printed: a loopback
// callback URL carrying an authorization code. Anything else would turn this into
// a way to make the CLI request an arbitrary address.
func parseLoopbackCallback(raw string) (*url.URL, error) {
	target, err := url.Parse(strings.Trim(strings.TrimSpace(raw), `"'`))
	if err != nil {
		return nil, fmt.Errorf("parse the callback URL: %w", err)
	}

	if target.Scheme != "http" {
		return nil, fmt.Errorf("callback URL must be http, got %q", target.Scheme)
	}
	if !isLoopbackHost(target.Hostname()) {
		return nil, fmt.Errorf("callback URL must point at the loopback address `auth login` printed, got host %q", target.Hostname())
	}
	if target.Port() == "" {
		return nil, errors.New("callback URL has no port — copy the whole address, including the :NNNNN")
	}
	if target.Path != "/callback" {
		return nil, fmt.Errorf("callback URL path must be /callback, got %q", target.Path)
	}

	q := target.Query()
	if q.Get("code") == "" && q.Get("error") == "" {
		return nil, errors.New("callback URL carries no code or error parameter — copy it from the browser's address bar after signing in")
	}

	return target, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}
