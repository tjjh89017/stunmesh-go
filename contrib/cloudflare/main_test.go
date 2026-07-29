package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/cloudflare/cloudflare-go"
	"github.com/tjjh89017/stunmesh-go/pluginapi"
)

func TestGetRecordName(t *testing.T) {
	const key = "3061b8fcbdb6972059518f1adc3590dca6a5f352"

	t.Run("should derive record name directly from key when no subdomain is set", func(t *testing.T) {
		p := &CloudflarePlugin{zoneName: "example.com"}

		got := p.getRecordName(key)

		want := key + ".example.com"
		if got != want {
			t.Errorf("getRecordName() = %q, want %q", got, want)
		}
	})

	t.Run("should derive record name directly from key when subdomain is set", func(t *testing.T) {
		p := &CloudflarePlugin{zoneName: "example.com", subdomain: "stunmesh"}

		got := p.getRecordName(key)

		want := key + ".stunmesh.example.com"
		if got != want {
			t.Errorf("getRecordName() = %q, want %q", got, want)
		}
	})
}

// fakeCloudflareAPI is a minimal in-memory stand-in for the Cloudflare DNS
// API, driven over real HTTP via cloudflare.BaseURL. It exists so Get/Set
// can be exercised end to end (request URL, record naming, response
// decoding) without a live Cloudflare account. NewCloudflarePlugin itself
// is not used here because it dials the real API during zone lookup before
// stdin is ever read, and main.go has no flag to redirect that lookup to a
// mock server, so a full process-spawn test cannot run offline; the
// process-level test below (TestMain_MissingFlags) instead exercises the
// path that fails before any network call is made, and
// TestMain_MalformedStdinJSON exercises stdin parsing directly against
// handleRequest with a plugin built from this fake, bypassing
// NewCloudflarePlugin the same way the tests above do.
type fakeCloudflareAPI struct {
	// requestedNames records every DNS record name looked up or written,
	// in call order. This is what would have caught the double-hash bug: a plugin
	// that hashed the key a second time would request a different name
	// than its siblings, and this slice would show it.
	requestedNames []string
	records        map[string]cloudflare.DNSRecord
}

func newFakeCloudflareAPI() *fakeCloudflareAPI {
	return &fakeCloudflareAPI{records: map[string]cloudflare.DNSRecord{}}
}

func (f *fakeCloudflareAPI) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/zones/test-zone/dns_records", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			name := r.URL.Query().Get("name")
			f.requestedNames = append(f.requestedNames, name)
			var result []cloudflare.DNSRecord
			if rec, ok := f.records[name]; ok {
				result = []cloudflare.DNSRecord{rec}
			}
			writeJSON(w, cloudflare.DNSListResponse{
				Response:   cloudflare.Response{Success: true},
				Result:     result,
				ResultInfo: cloudflare.ResultInfo{Count: len(result)},
			})
		case http.MethodPost:
			var body cloudflare.CreateDNSRecordParams
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.requestedNames = append(f.requestedNames, body.Name)
			rec := cloudflare.DNSRecord{ID: "rec-1", Type: body.Type, Name: body.Name, Content: body.Content}
			f.records[body.Name] = rec
			writeJSON(w, cloudflare.DNSRecordResponse{Response: cloudflare.Response{Success: true}, Result: rec})
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/zones/test-zone/dns_records/rec-1", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			var body cloudflare.UpdateDNSRecordParams
			_ = json.NewDecoder(r.Body).Decode(&body)
			for name, rec := range f.records {
				if rec.ID == "rec-1" {
					rec.Content = body.Content
					f.records[name] = rec
				}
			}
			writeJSON(w, cloudflare.DNSRecordResponse{Response: cloudflare.Response{Success: true}})
		default:
			http.NotFound(w, r)
		}
	})
	return httptest.NewServer(mux)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func newTestPlugin(t *testing.T, baseURL string) *CloudflarePlugin {
	t.Helper()
	api, err := cloudflare.NewWithAPIToken("test-token", cloudflare.BaseURL(baseURL))
	if err != nil {
		t.Fatalf("cloudflare.NewWithAPIToken() error = %v", err)
	}
	return &CloudflarePlugin{
		api:       api,
		zoneID:    &cloudflare.ResourceContainer{Identifier: "test-zone"},
		zoneName:  "example.com",
		subdomain: "stunmesh",
	}
}

func TestCloudflarePlugin_SetThenGet_HappyPath(t *testing.T) {
	const key = "3061b8fcbdb6972059518f1adc3590dca6a5f352"
	fake := newFakeCloudflareAPI()
	srv := fake.server()
	defer srv.Close()
	p := newTestPlugin(t, srv.URL)

	if err := p.Set(context.Background(), key, "encrypted-hex-value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, err := p.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "encrypted-hex-value" {
		t.Errorf("Get() = %q, want %q", got, "encrypted-hex-value")
	}

	wantName := key + ".stunmesh.example.com"
	for _, name := range fake.requestedNames {
		if name != wantName {
			t.Errorf("plugin requested DNS record name %q, want %q (a double-hashed key, as in the old bug, would diverge here)", name, wantName)
		}
	}
}

func TestCloudflarePlugin_Get_NotFound(t *testing.T) {
	const key = "3061b8fcbdb6972059518f1adc3590dca6a5f352"
	fake := newFakeCloudflareAPI()
	srv := fake.server()
	defer srv.Close()
	p := newTestPlugin(t, srv.URL)

	_, err := p.Get(context.Background(), key)
	if err == nil {
		t.Fatal("Get() error = nil, want error for missing record")
	}
}

func TestCloudflarePlugin_Set_UpdatesExistingRecord(t *testing.T) {
	const key = "3061b8fcbdb6972059518f1adc3590dca6a5f352"
	fake := newFakeCloudflareAPI()
	srv := fake.server()
	defer srv.Close()
	p := newTestPlugin(t, srv.URL)

	if err := p.Set(context.Background(), key, "first-value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := p.Set(context.Background(), key, "second-value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, err := p.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "second-value" {
		t.Errorf("Get() = %q, want %q (update should have overwritten the record)", got, "second-value")
	}
}

// TestRespondEnvelopes checks the wire-level shape of the exec protocol
// responses this plugin emits, matching pluginapi.ExecResponse (the same
// struct the ExecPlugin caller in internal/plugin decodes).
func TestRespondEnvelopes(t *testing.T) {
	t.Run("success envelope", func(t *testing.T) {
		data, err := json.Marshal(pluginapi.ExecResponse{Success: true, Value: "abc"})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		var resp pluginapi.ExecResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if !resp.Success || resp.Value != "abc" || resp.Error != "" {
			t.Errorf("round-tripped response = %+v", resp)
		}
	})

	t.Run("error envelope", func(t *testing.T) {
		data, err := json.Marshal(pluginapi.ExecResponse{Success: false, Error: "boom"})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		var resp pluginapi.ExecResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if resp.Success || resp.Error != "boom" || resp.Value != "" {
			t.Errorf("round-tripped response = %+v", resp)
		}
	})
}

// TestMain_MissingFlags is a real process-level smoke test: it builds and
// runs the actual binary, the same way exec_test_plugin.sh is driven from
// internal/plugin/exec_test.go, and asserts on its stdout envelope and exit
// code. It deliberately omits -zone/-token, which main() rejects before
// ever touching the network or reading stdin, so the test can run offline.
func TestMain_MissingFlags(t *testing.T) {
	bin := buildCloudflarePlugin(t)

	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(`{"action":"get","key":"abc"}`)
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("expected non-zero exit, got success; output=%s", out)
	}

	var resp pluginapi.ExecResponse
	if jsonErr := json.Unmarshal(out, &resp); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v; output=%s", jsonErr, out)
	}
	if resp.Success {
		t.Errorf("resp.Success = true, want false; output=%s", out)
	}
	if resp.Error == "" {
		t.Error("resp.Error is empty, want a message explaining the missing flags")
	}
}

// TestMain_MalformedStdinJSON drives handleRequest -- the seam main()
// dispatches to once NewCloudflarePlugin has succeeded -- directly with a
// plugin pointed at the fake server, so this test exercises the
// json.Unmarshal failure branch (not flag validation, which lives entirely
// in main() and is never called here, and not NewCloudflarePlugin's real
// network call, which is bypassed the same way newTestPlugin bypasses it
// elsewhere in this file).
func TestMain_MalformedStdinJSON(t *testing.T) {
	fake := newFakeCloudflareAPI()
	srv := fake.server()
	defer srv.Close()
	p := newTestPlugin(t, srv.URL)

	var stdout bytes.Buffer
	exitCode := handleRequest(context.Background(), p, strings.NewReader(`{not valid json`), &stdout)

	if exitCode == 0 {
		t.Fatalf("handleRequest() exit code = 0, want non-zero for malformed JSON; output=%s", stdout.String())
	}

	var resp pluginapi.ExecResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v; output=%s", err, stdout.String())
	}
	if resp.Success {
		t.Errorf("resp.Success = true, want false; output=%s", stdout.String())
	}
	if resp.Error == "" {
		t.Error("resp.Error is empty, want a message explaining the malformed JSON")
	}
}

func buildCloudflarePlugin(t *testing.T) string {
	t.Helper()
	bin := t.TempDir() + "/stunmesh-cloudflare-test"
	cmd := exec.Command("go", "build", "-o", bin, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}
