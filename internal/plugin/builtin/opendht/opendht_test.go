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
	"slices"
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
		_, _ = fmt.Fprintln(w, line(t, defaultMagic, 200, "newest"))
		_, _ = fmt.Fprintln(w, line(t, defaultMagic, 100, "oldest"))
		_, _ = fmt.Fprintln(w, line(t, defaultMagic, 150, "middle"))
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
		_, _ = fmt.Fprintln(w, rawLine(t, "total garbage not json"))
		_, _ = fmt.Fprintln(w, line(t, "someone-else", 1<<40, "hijacked"))
		_, _ = fmt.Fprintln(w, line(t, defaultMagic, 100, "ours"))
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
		_, _ = fmt.Fprintln(w, line(t, "someone-else", 100, "theirs"))
	}))
	defer server.Close()

	_, err := newTestPlugin(t, server.URL).Get(context.Background(), testKey)
	if err == nil {
		t.Fatal("expected an error when no value carries our magic")
	}
}

func TestGetRespectsConfiguredMagic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, line(t, defaultMagic, 200, "default-magic"))
		_, _ = fmt.Fprintln(w, line(t, "custom", 100, "custom-magic"))
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
		_, _ = fmt.Fprintln(w, stored)
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
	p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{configKeyEndpoint: "https://a.example"})
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}

	plugin, ok := p.(*OpenDHTPlugin)
	if !ok {
		t.Fatal("expected an *OpenDHTPlugin")
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
			p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{
				configKeyEndpoint: "https://a.example",
				configKeyTimeout:  tt.value,
			})
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
		config := pluginapi.PluginConfig{
			configKeyEndpoint: "https://a.example",
			configKeyTimeout:  value,
		}
		if _, err := NewOpenDHTPlugin(config); err == nil {
			t.Errorf("expected an error for timeout %v", value)
		}
	}
}

// A scheme-less endpoint used to be accepted and then fail on every request
// with net/http's "unsupported protocol scheme", long after startup.
func TestEndpointRequiresScheme(t *testing.T) {
	rejected := []string{
		"dhtproxy.jami.net",
		"dhtproxy.jami.net:80",
		"//dhtproxy.jami.net",
		"ftp://dhtproxy.jami.net",
		"https://",
		"   ",
	}

	for _, endpoint := range rejected {
		t.Run(endpoint, func(t *testing.T) {
			_, err := NewOpenDHTPlugin(pluginapi.PluginConfig{configKeyEndpoint: endpoint})
			if err == nil {
				t.Fatalf("expected %q to be rejected", endpoint)
			}
			if !strings.Contains(err.Error(), "http:// or https://") {
				t.Errorf("error should say what is required, got: %v", err)
			}
		})
	}

	accepted := []string{
		"http://127.0.0.1:8080",
		"https://dhtproxy.jami.net",
		"https://dhtproxy.jami.net:443",
	}

	for _, endpoint := range accepted {
		t.Run(endpoint, func(t *testing.T) {
			p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{configKeyEndpoint: endpoint})
			if err != nil {
				t.Fatalf("expected %q to be accepted, got: %v", endpoint, err)
			}
			if got := p.(*OpenDHTPlugin).endpoints; len(got) != 1 || got[0] != endpoint {
				t.Errorf("endpoints = %v, want [%q]", got, endpoint)
			}
		})
	}
}

// A trailing slash would make the request path "//key/...".
func TestEndpointTrailingSlashTrimmed(t *testing.T) {
	p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{configKeyEndpoint: "http://127.0.0.1:8080/"})
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}

	if got := p.(*OpenDHTPlugin).endpoints; len(got) != 1 || got[0] != "http://127.0.0.1:8080" {
		t.Errorf("endpoints = %v, want the trailing slash removed", got)
	}
}

func TestResolveEndpointsMergesAndDeduplicates(t *testing.T) {
	tests := []struct {
		name   string
		config pluginapi.PluginConfig
		want   []string
	}{
		{
			name:   "singular only",
			config: pluginapi.PluginConfig{configKeyEndpoint: "https://a.example"},
			want:   []string{"https://a.example"},
		},
		{
			name:   "list only, order preserved",
			config: pluginapi.PluginConfig{configKeyEndpoints: []interface{}{"https://a.example", "https://b.example"}},
			want:   []string{"https://a.example", "https://b.example"},
		},
		{
			name: "singular comes first",
			config: pluginapi.PluginConfig{
				configKeyEndpoint:  "https://first.example",
				configKeyEndpoints: []interface{}{"https://b.example"},
			},
			want: []string{"https://first.example", "https://b.example"},
		},
		{
			name: "duplicates collapse, first position wins",
			config: pluginapi.PluginConfig{
				configKeyEndpoint:  "https://a.example",
				configKeyEndpoints: []interface{}{"https://b.example", "https://a.example/"},
			},
			want: []string{"https://a.example", "https://b.example"},
		},
		{
			name:   "already []string",
			config: pluginapi.PluginConfig{configKeyEndpoints: []string{"https://a.example"}},
			want:   []string{"https://a.example"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewOpenDHTPlugin(tt.config)
			if err != nil {
				t.Fatalf("failed to create plugin: %v", err)
			}

			got := p.(*OpenDHTPlugin).endpoints
			if !slices.Equal(got, tt.want) {
				t.Errorf("endpoints = %v, want %v", got, tt.want)
			}
		})
	}
}

// A bad entry anywhere in the list must fail at startup, not when the earlier
// endpoints happen to go down.
func TestResolveEndpointsValidatesEveryEntry(t *testing.T) {
	_, err := NewOpenDHTPlugin(pluginapi.PluginConfig{
		configKeyEndpoints: []interface{}{"https://good.example", "bad.example"},
	})
	if err == nil {
		t.Fatal("expected the scheme-less second entry to be rejected")
	}
	if !strings.Contains(err.Error(), "bad.example") {
		t.Errorf("error should name the offending entry, got: %v", err)
	}
}

// There is no built-in default: an unconfigured plugin must say so rather
// than silently adopt somebody else's proxy.
func TestNoEndpointConfigured(t *testing.T) {
	_, err := NewOpenDHTPlugin(pluginapi.PluginConfig{})
	if err == nil {
		t.Fatal("expected an error when neither endpoint nor endpoints is set")
	}
	if !strings.Contains(err.Error(), configKeyEndpoints) {
		t.Errorf("error should name the config key, got: %v", err)
	}
}

func TestResolveEndpointsRejectsNonList(t *testing.T) {
	_, err := NewOpenDHTPlugin(pluginapi.PluginConfig{configKeyEndpoints: 42})
	if err == nil {
		t.Fatal("expected a non-list endpoints value to be rejected")
	}
}

// Both Get and Set skip a failing endpoint and keep the first that answers.
func TestFailsOverToNextEndpoint(t *testing.T) {
	for _, action := range []string{"get", "set"} {
		t.Run(action, func(t *testing.T) {
			dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			}))
			defer dead.Close()

			var liveHits int
			live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				liveHits++
				if r.Method == http.MethodGet {
					_, _ = fmt.Fprintln(w, line(t, defaultMagic, 1, "payload"))
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer live.Close()

			p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{
				configKeyEndpoints: []interface{}{dead.URL, live.URL},
			})
			if err != nil {
				t.Fatalf("failed to create plugin: %v", err)
			}

			if action == "get" {
				got, err := p.Get(context.Background(), testKey)
				if err != nil {
					t.Fatalf("Get() error = %v, want the second endpoint to answer", err)
				}
				if got != "payload" {
					t.Errorf("Get() = %q, want payload", got)
				}
			} else if err := p.Set(context.Background(), testKey, "payload"); err != nil {
				t.Fatalf("Set() error = %v, want the second endpoint to accept", err)
			}

			if liveHits != 1 {
				t.Errorf("live endpoint hit %d times, want 1", liveHits)
			}
		})
	}
}

// The first endpoint that answers ends the walk.
func TestDoesNotTryLaterEndpointsOnSuccess(t *testing.T) {
	var secondHits int
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits++
	}))
	defer second.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, line(t, defaultMagic, 1, "payload"))
	}))
	defer first.Close()

	p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{
		configKeyEndpoints: []interface{}{first.URL, second.URL},
	})
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}

	if _, err := p.Get(context.Background(), testKey); err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if secondHits != 0 {
		t.Errorf("second endpoint hit %d times, want 0", secondHits)
	}
}

// A key with no value is an answer, not a failure: the walk stops there.
func TestNoValueDoesNotFailOver(t *testing.T) {
	var secondHits int
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits++
	}))
	defer second.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer first.Close()

	p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{
		configKeyEndpoints: []interface{}{first.URL, second.URL},
	})
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}

	if _, err := p.Get(context.Background(), testKey); err == nil {
		t.Fatal("Get() error = nil, want no value found")
	}

	if secondHits != 0 {
		t.Errorf("second endpoint hit %d times, want 0", secondHits)
	}
}

// When every endpoint fails the error has to name them all, or a mesh that is
// down for two different reasons looks like it is down for one.
func TestAllEndpointsFailingReportsEach(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "first is unhappy", http.StatusInternalServerError)
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "second is unhappy", http.StatusBadGateway)
	}))
	defer second.Close()

	p, err := NewOpenDHTPlugin(pluginapi.PluginConfig{
		configKeyEndpoints: []interface{}{first.URL, second.URL},
	})
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}

	err = p.Set(context.Background(), testKey, "payload")
	if err == nil {
		t.Fatal("Set() error = nil, want both endpoints reported")
	}

	for _, want := range []string{first.URL, second.URL, "first is unhappy", "second is unhappy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}
