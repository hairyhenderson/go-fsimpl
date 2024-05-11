package integration

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

// waitForURL - waits up to 20s for a given URL to respond with a 200
func waitForURL(ctx context.Context, t *testing.T, url string) error {
	client := http.DefaultClient

	retries := 100
	for retries > 0 {
		retries--

		time.Sleep(200 * time.Millisecond)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		assert.NoError(t, err)

		resp, err := client.Do(req)
		if err != nil {
			t.Logf("Got error, retries left: %d (error: %v)", retries, err)

			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Logf("Body is: %s", body)

			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return nil
		}
	}

	return nil
}
