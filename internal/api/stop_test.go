package api

import "testing"

// POST /vault/stop asks the server to lock and exit, as SIGTERM does, but
// only over the private channel: over TCP it doesn't exist.
func TestStop_OnlyOverPrivateChannel(t *testing.T) {
	env := setup(t)
	stopped := func() bool {
		select {
		case <-env.server.Stopped():
			return true
		default:
			return false
		}
	}
	if w := env.call("POST", "/vault/stop", "", nil, false); w.Code != 404 {
		t.Fatalf("over TCP: %d %s", w.Code, w.Body)
	}
	if stopped() {
		t.Fatal("stopped over TCP")
	}
	// No password, like SIGTERM; asking twice is harmless.
	for range 2 {
		if w := env.call("POST", "/vault/stop", "", nil, true); w.Code != 202 {
			t.Fatalf("over the socket: %d %s", w.Code, w.Body)
		}
	}
	if !stopped() {
		t.Fatal("not stopped")
	}
}
