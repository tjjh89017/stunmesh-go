package passthrough

import (
	"context"
	"errors"
	"testing"

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

func TestPassthroughForwardsUnchanged(t *testing.T) {
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

func TestPassthroughForwardsErrors(t *testing.T) {
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

func TestPassthroughConfigErrors(t *testing.T) {
	if _, err := New(context.Background(), pluginapi.PluginConfig{}, fakeStores{}); err == nil {
		t.Error("missing next: want error")
	}
	if _, err := New(context.Background(), pluginapi.PluginConfig{"next": "absent"}, fakeStores{}); err == nil {
		t.Error("undefined next: want error")
	}
}
