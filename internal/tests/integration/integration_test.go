package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

// freeport - find a free TCP port for immediate use. No guarantees!
func freeport(t *testing.T) (port int, addr string) {
	t.Helper()

	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}

	defer l.Close()

	a := l.Addr().(*net.TCPAddr)

	return a.Port, a.String()
}

// requireCommand skips the test when the named command isn't on the PATH. In
// CI the command must be present, so the test fails instead.
func requireCommand(t *testing.T, name string) {
	t.Helper()

	if _, err := exec.LookPath(name); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s is required in CI: %v", name, err)
		}

		t.Skipf("skipping: %s not found: %v", name, err)
	}
}

// waitForURL - waits up to 20s for a given URL to respond with a 200
func waitForURL(ctx context.Context, t *testing.T, url string) error {
	t.Helper()

	return waitForURLBody(ctx, t, url, func([]byte) error { return nil })
}

// waitForURLBody - waits up to 20s for a given URL to respond with a 200 and
// a body that passes check
func waitForURLBody(ctx context.Context, t *testing.T, url string, check func(body []byte) error) error {
	t.Helper()

	return poll(ctx, t, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		return check(body)
	})
}

// waitForPort - waits up to 20s for a TCP listener at addr
func waitForPort(ctx context.Context, t *testing.T, addr string) error {
	t.Helper()

	d := net.Dialer{}

	return poll(ctx, t, func() error {
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}

		return conn.Close()
	})
}

// poll calls check until it succeeds, checking often so servers that start
// quickly don't hold up the test, and gives up after 20s.
func poll(ctx context.Context, t *testing.T, check func() error) error {
	t.Helper()

	const (
		interval = 25 * time.Millisecond
		timeout  = 20 * time.Second
	)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		err := check()
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("gave up after %s: %w", timeout, err)
		case <-ticker.C:
		}
	}
}
