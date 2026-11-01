package mirror

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

type fakeStore struct {
	value  string
	err    error
	gets   int
	stored []string
}

func (f *fakeStore) Get(context.Context, pluginapi.PluginRequest) (string, error) {
	f.gets++
	return f.value, f.err
}

func (f *fakeStore) Set(_ context.Context, req pluginapi.PluginRequest) error {
	if f.err != nil {
		return f.err
	}
	f.stored = append(f.stored, req.Value)
	return nil
}

type fakeStores map[string]pluginapi.Store

func (s fakeStores) Lookup(name string) (pluginapi.Store, error) {
	store, ok := s[name]
	if !ok {
		return nil, errors.New("not defined: " + name)
	}
	return store, nil
}

func newMirror(t *testing.T, stores fakeStores, names ...interface{}) pluginapi.Store {
	t.Helper()
	p, err := New(context.Background(), pluginapi.PluginConfig{"stores": names}, stores)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMirrorSetWritesAll(t *testing.T) {
	a, b := &fakeStore{}, &fakeStore{}
	p := newMirror(t, fakeStores{"a": a, "b": b}, "a", "b")

	if err := p.Set(context.Background(), pluginapi.PluginRequest{Key: "k", Value: "v"}); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]*fakeStore{"a": a, "b": b} {
		if len(s.stored) != 1 || s.stored[0] != "v" {
			t.Errorf("%s stored %v, want [v]", name, s.stored)
		}
	}
}

func TestMirrorSetJoinsAllErrors(t *testing.T) {
	errA, errC := errors.New("a down"), errors.New("c down")
	b := &fakeStore{}
	p := newMirror(t, fakeStores{"a": &fakeStore{err: errA}, "b": b, "c": &fakeStore{err: errC}}, "a", "b", "c")

	err := p.Set(context.Background(), pluginapi.PluginRequest{Key: "k", Value: "v"})
	if !errors.Is(err, errA) || !errors.Is(err, errC) {
		t.Fatalf("Set error = %v, want both a and c errors", err)
	}
	if len(b.stored) != 1 {
		t.Errorf("b stored %v; a failure must not stop the other writes", b.stored)
	}
}

func TestMirrorGetOrdered(t *testing.T) {
	a, b := &fakeStore{value: "from-a"}, &fakeStore{value: "from-b"}
	p := newMirror(t, fakeStores{"a": a, "b": b}, "b", "a")

	got, err := p.Get(context.Background(), pluginapi.PluginRequest{Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-b" {
		t.Errorf("Get = %q, want from-b (first listed)", got)
	}
	if a.gets != 0 {
		t.Errorf("a asked %d times after b answered", a.gets)
	}
}

func TestMirrorGetFallsThrough(t *testing.T) {
	p := newMirror(t, fakeStores{
		"empty":  &fakeStore{},
		"broken": &fakeStore{err: errors.New("down")},
		"good":   &fakeStore{value: "v"},
	}, "empty", "broken", "good")

	got, err := p.Get(context.Background(), pluginapi.PluginRequest{Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "v" {
		t.Errorf("Get = %q, want v", got)
	}
}

func TestMirrorGetAllFail(t *testing.T) {
	errB := errors.New("b down")
	p := newMirror(t, fakeStores{"a": &fakeStore{}, "b": &fakeStore{err: errB}}, "a", "b")

	got, err := p.Get(context.Background(), pluginapi.PluginRequest{Key: "k"})
	if err == nil || got != "" {
		t.Fatalf("Get = %q, %v; want an error", got, err)
	}
	if !errors.Is(err, errB) {
		t.Errorf("Get error = %v, want it to carry b's error", err)
	}
}

func TestMirrorConfigErrors(t *testing.T) {
	for name, config := range map[string]pluginapi.PluginConfig{
		"missing":   {},
		"empty":     {"stores": []interface{}{}},
		"not list":  {"stores": 3},
		"undefined": {"stores": []interface{}{"absent"}},
	} {
		if _, err := New(context.Background(), config, fakeStores{}); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestMirrorGetLogsEarlierFailure(t *testing.T) {
	var buf bytes.Buffer
	ctx := zerolog.New(&buf).Level(zerolog.DebugLevel).WithContext(context.Background())
	p := newMirror(t, fakeStores{"broken": &fakeStore{err: errors.New("down")}, "good": &fakeStore{value: "v"}}, "broken", "good")

	if _, err := p.Get(ctx, pluginapi.PluginRequest{Key: "k"}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, `"store":"broken"`) || !strings.Contains(out, "down") {
		t.Errorf("debug log = %q, want the broken store and its error", out)
	}
}
