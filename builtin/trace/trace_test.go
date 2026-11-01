package trace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

type fakeStore struct {
	gets []pluginapi.PluginRequest
	sets []pluginapi.PluginRequest
	err  error
}

func (f *fakeStore) Get(_ context.Context, req pluginapi.PluginRequest) (string, error) {
	f.gets = append(f.gets, req)
	return "value-" + req.Key, f.err
}

func (f *fakeStore) Set(_ context.Context, req pluginapi.PluginRequest) error {
	f.sets = append(f.sets, req)
	return f.err
}

type fakeStores map[string]pluginapi.Store

func (s fakeStores) Lookup(name string) (pluginapi.Store, error) {
	store, ok := s[name]
	if !ok {
		return nil, errors.New("not defined: " + name)
	}
	return store, nil
}

func TestTraceForwardsUnchanged(t *testing.T) {
	inner := &fakeStore{}
	p, err := New(context.Background(), pluginapi.PluginConfig{"next": "inner"}, fakeStores{"inner": inner})
	if err != nil {
		t.Fatal(err)
	}

	req := pluginapi.PluginRequest{
		Device:         "wg0",
		LocalPublicKey: pluginapi.Key{1},
		PeerPublicKey:  pluginapi.Key{2},
		Key:            "k",
		Value:          "v",
	}
	if err := p.Set(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got != "value-k" {
		t.Errorf("Get = %q, want value-k", got)
	}
	if len(inner.sets) != 1 || inner.sets[0] != req {
		t.Errorf("inner Set got %+v, want %+v", inner.sets, req)
	}
	if len(inner.gets) != 1 || inner.gets[0] != req {
		t.Errorf("inner Get got %+v, want %+v", inner.gets, req)
	}
}

func TestTraceForwardsErrors(t *testing.T) {
	boom := errors.New("boom")
	p, err := New(context.Background(), pluginapi.PluginConfig{"next": "inner"}, fakeStores{"inner": &fakeStore{err: boom}})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Set(context.Background(), pluginapi.PluginRequest{Key: "k"}); !errors.Is(err, boom) {
		t.Errorf("Set error = %v, want boom", err)
	}
	if _, err := p.Get(context.Background(), pluginapi.PluginRequest{Key: "k"}); !errors.Is(err, boom) {
		t.Errorf("Get error = %v, want boom", err)
	}
}

func TestTraceConfigErrors(t *testing.T) {
	if _, err := New(context.Background(), pluginapi.PluginConfig{}, fakeStores{}); err == nil {
		t.Error("missing next: want error")
	}
	if _, err := New(context.Background(), pluginapi.PluginConfig{"next": "absent"}, fakeStores{}); err == nil {
		t.Error("undefined next: want error")
	}
}

func readRecords(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		var r map[string]any
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("line %q is not JSON: %v", sc.Text(), err)
		}
		out = append(out, r)
	}
	return out
}

func TestTraceFileRecordsEachCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	p, err := New(context.Background(), pluginapi.PluginConfig{"next": "inner", "file": path}, fakeStores{"inner": &fakeStore{}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.(*plugin).Close() }()

	req := pluginapi.PluginRequest{Device: "wg0", LocalPublicKey: pluginapi.Key{1}, PeerPublicKey: pluginapi.Key{2}, Key: "k", Value: "secret-value"}
	if err := p.Set(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), pluginapi.PluginRequest{Device: "wg0", Key: "k"}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "secret-value") {
		t.Errorf("trace file leaks the value: %s", data)
	}
	recs := readRecords(t, path)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2: %s", len(recs), data)
	}
	set, get := recs[0], recs[1]
	for field, want := range map[string]any{
		"op":               "set",
		"device":           "wg0",
		"local_public_key": pluginapi.Key{1}.String(),
		"peer_public_key":  pluginapi.Key{2}.String(),
		"key":              "k",
		"value_len":        float64(len("secret-value")),
		"ok":               true,
	} {
		if set[field] != want {
			t.Errorf("set %s = %v, want %v", field, set[field], want)
		}
	}
	if _, err := time.Parse(time.RFC3339, set["time"].(string)); err != nil {
		t.Errorf("set time: %v", err)
	}
	if _, ok := set["duration_ms"].(float64); !ok {
		t.Errorf("set duration_ms = %v, want a number", set["duration_ms"])
	}
	for _, field := range []string{"value", "error"} {
		if _, ok := set[field]; ok {
			t.Errorf("set record has %q", field)
		}
	}
	if get["op"] != "get" || get["value_len"] != float64(len("value-k")) {
		t.Errorf("get record = %v, want op get, value_len %d", get, len("value-k"))
	}
	for _, field := range []string{"local_public_key", "peer_public_key"} {
		if _, ok := get[field]; ok {
			t.Errorf("get record has %q although the key is zero", field)
		}
	}
}

func TestTraceRecordsInnerError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	boom := errors.New("boom")
	p, err := New(context.Background(), pluginapi.PluginConfig{"next": "inner", "file": path}, fakeStores{"inner": &fakeStore{err: boom}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.(*plugin).Close() }()

	if err := p.Set(context.Background(), pluginapi.PluginRequest{Key: "k"}); !errors.Is(err, boom) {
		t.Errorf("Set error = %v, want boom", err)
	}
	recs := readRecords(t, path)
	if len(recs) != 1 || recs[0]["ok"] != false || recs[0]["error"] != "boom" {
		t.Errorf("records = %v, want one with ok=false error=boom", recs)
	}
}

func TestTraceCloseClosesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	p, err := New(context.Background(), pluginapi.PluginConfig{"next": "inner", "file": path}, fakeStores{"inner": &fakeStore{}})
	if err != nil {
		t.Fatal(err)
	}
	tp := p.(*plugin)
	f := tp.file
	if err := tp.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("write after Close = %v, want os.ErrClosed", err)
	}
	if err := tp.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
}

func TestTraceLogsAtDebugWithoutFile(t *testing.T) {
	var buf bytes.Buffer
	ctx := zerolog.New(&buf).Level(zerolog.DebugLevel).WithContext(context.Background())
	p, err := New(ctx, pluginapi.PluginConfig{"next": "inner"}, fakeStores{"inner": &fakeStore{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Set(ctx, pluginapi.PluginRequest{Key: "k", Value: "secret-value"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"op":"set"`) || !strings.Contains(out, `"value_len":12`) || strings.Contains(out, "secret-value") {
		t.Errorf("debug log = %q, want op set, value_len 12 and no value", out)
	}
}
