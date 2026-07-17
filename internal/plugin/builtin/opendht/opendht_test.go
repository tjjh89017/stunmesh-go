//go:build builtin_opendht

package opendht

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

const testKey = "3061b8fcbdb6972059518f1adc3590dca6a5f352"

// line renders one value object the way the proxy reports it: the envelope,
// base64-encoded, inside a JSON object, one per line.
func line(t *testing.T, magic string, ts int64, data string) string {
	t.Helper()

	payload, err := json.Marshal(&envelope{Magic: magic, Ts: ts, Data: data})
	if err != nil {
		t.Fatalf("failed to marshal envelope: %v", err)
	}

	body, err := json.Marshal(map[string]string{
		"data": base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil {
		t.Fatalf("failed to marshal value: %v", err)
	}

	return string(body)
}

// rawLine renders a value object holding arbitrary bytes rather than an
// envelope, for the values a third party may have published.
func rawLine(t *testing.T, data string) string {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"data": base64.StdEncoding.EncodeToString([]byte(data)),
	})
	if err != nil {
		t.Fatalf("failed to marshal value: %v", err)
	}

	return string(body)
}

func newTestPlugin(t *testing.T, url string) pluginapi.Store {
	t.Helper()

	p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{configKeyEndpoint: url})
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}

	return p
}

func TestGetReturnsMostRecentValue(t *testing.T) {
	// A key holds a set of values, and publishing every cycle against a
	// 10-minute expiry leaves several of our own under it at once. The proxy
	// does not order them, so the newest must be selected by ts.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, line(t, defaultMagic, 200, "newest"))
		fmt.Fprintln(w, line(t, defaultMagic, 100, "oldest"))
		fmt.Fprintln(w, line(t, defaultMagic, 150, "middle"))
	}))
	defer server.Close()

	got, err := newTestPlugin(t, server.URL).Get(context.Background(), testKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != "newest" {
		t.Errorf("expected newest, got %q", got)
	}
}

func TestGetIgnoresForeignValues(t *testing.T) {
	// Anyone may publish under a known key. Values that are not our envelope
	// must not be returned -- not even one whose ts would otherwise win.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, rawLine(t, "total garbage not json"))
		fmt.Fprintln(w, line(t, "someone-else", 1<<40, "hijacked"))
		fmt.Fprintln(w, line(t, defaultMagic, 100, "ours"))
	}))
	defer server.Close()

	got, err := newTestPlugin(t, server.URL).Get(context.Background(), testKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != "ours" {
		t.Errorf("expected ours, got %q", got)
	}
}

func TestGetEmptyKey(t *testing.T) {
	// A key with no values answers 200 with an empty body.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	_, err := newTestPlugin(t, server.URL).Get(context.Background(), testKey)
	if err == nil {
		t.Fatal("expected an error for a key with no values")
	}
}

func TestGetOnlyForeignValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, line(t, "someone-else", 100, "theirs"))
	}))
	defer server.Close()

	_, err := newTestPlugin(t, server.URL).Get(context.Background(), testKey)
	if err == nil {
		t.Fatal("expected an error when no value carries our magic")
	}
}

func TestGetRespectsConfiguredMagic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, line(t, defaultMagic, 200, "default-magic"))
		fmt.Fprintln(w, line(t, "custom", 100, "custom-magic"))
	}))
	defer server.Close()

	p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{
		configKeyEndpoint: server.URL,
		configKeyMagic:    "custom",
	})
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}

	got, err := p.Get(context.Background(), testKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != "custom-magic" {
		t.Errorf("expected custom-magic, got %q", got)
	}
}

func TestGetServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := newTestPlugin(t, server.URL).Get(context.Background(), testKey)
	if err == nil {
		t.Fatal("expected an error for a 502 response")
	}
}

func TestSetPublishesEnvelope(t *testing.T) {
	var gotPath, gotMethod string
	var gotEnvelope envelope

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
			return
		}

		var v value
		if err := json.Unmarshal(body, &v); err != nil {
			t.Errorf("body is not a value object: %v", err)
			return
		}

		raw, err := base64.StdEncoding.DecodeString(v.Data)
		if err != nil {
			t.Errorf("data is not base64: %v", err)
			return
		}

		if err := json.Unmarshal(raw, &gotEnvelope); err != nil {
			t.Errorf("data is not an envelope: %v", err)
		}
	}))
	defer server.Close()

	before := time.Now().Unix()
	if err := newTestPlugin(t, server.URL).Set(context.Background(), testKey, "payload"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now().Unix()

	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}

	if gotPath != "/key/"+testKey {
		t.Errorf("expected /key/%s, got %s", testKey, gotPath)
	}

	if gotEnvelope.Magic != defaultMagic {
		t.Errorf("expected magic %s, got %s", defaultMagic, gotEnvelope.Magic)
	}

	if gotEnvelope.Data != "payload" {
		t.Errorf("expected payload, got %q", gotEnvelope.Data)
	}

	if gotEnvelope.Ts < before || gotEnvelope.Ts > after {
		t.Errorf("expected ts within [%d, %d], got %d", before, after, gotEnvelope.Ts)
	}
}

func TestSetServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	err := newTestPlugin(t, server.URL).Set(context.Background(), testKey, "payload")
	if err == nil {
		t.Fatal("expected an error for a 502 response")
	}
}

func TestRoundTrip(t *testing.T) {
	// What Set publishes must be what Get reads back.
	var stored string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("failed to read body: %v", err)
				return
			}
			stored = string(body)
			return
		}
		fmt.Fprintln(w, stored)
	}))
	defer server.Close()

	p := newTestPlugin(t, server.URL)
	want := strings.Repeat("deadbeef", 75)

	if err := p.Set(context.Background(), testKey, want); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := p.Get(context.Background(), testKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != want {
		t.Errorf("round trip changed the value: got %d chars, want %d", len(got), len(want))
	}
}

func TestKeyMustBeInfoHash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a malformed key must not reach the proxy")
	}))
	defer server.Close()

	tests := []struct {
		name string
		key  string
	}{
		{"empty", ""},
		{"too short", "abc"},
		{"not hex", "3061b8fcbdb6972059518f1adc3590dca6a5f35z"},
		{"path traversal", "../../node/info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPlugin(t, server.URL)

			if _, err := p.Get(context.Background(), tt.key); err == nil {
				t.Error("expected Get to reject the key")
			}

			if err := p.Set(context.Background(), tt.key, "payload"); err == nil {
				t.Error("expected Set to reject the key")
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{})
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}

	plugin, ok := p.(*OpenDHTPlugin)
	if !ok {
		t.Fatal("expected an *OpenDHTPlugin")
	}

	if plugin.endpoint != defaultEndpoint {
		t.Errorf("expected endpoint %s, got %s", defaultEndpoint, plugin.endpoint)
	}

	if plugin.magic != defaultMagic {
		t.Errorf("expected magic %s, got %s", defaultMagic, plugin.magic)
	}

	if plugin.client.Timeout != defaultTimeout {
		t.Errorf("expected timeout %s, got %s", defaultTimeout, plugin.client.Timeout)
	}
}

func TestTimeoutConfig(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  time.Duration
	}{
		{"duration string", "20s", 20 * time.Second},
		{"seconds as int", 30, 30 * time.Second},
		{"seconds as float", float64(30), 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{configKeyTimeout: tt.value})
			if err != nil {
				t.Fatalf("failed to create plugin: %v", err)
			}

			if got := p.(*OpenDHTPlugin).client.Timeout; got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestInvalidTimeoutConfig(t *testing.T) {
	for _, value := range []interface{}{"not-a-duration", true} {
		if _, err := NewOpenDHTPlugin(pluginapi.PluginConfig{configKeyTimeout: value}); err == nil {
			t.Errorf("expected an error for timeout %v", value)
		}
	}
}
