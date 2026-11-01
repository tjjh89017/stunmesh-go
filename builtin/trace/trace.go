// Package trace is a working plugin and the reference example for a chaining
// plugin with one inner instance. It forwards every Get and Set unchanged to
// the instance named in its "next" config field and records each call, so
// wrapping any instance with trace shows what the core asks of it. With
// "file" set it appends one JSON line per call; otherwise it logs at debug.
package trace

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/pluginconfig"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

const (
	configKeyNext = "next"
	configKeyFile = "file"
)

func init() {
	pluginapi.RegisterWithStores("trace", New)
}

// plugin closes only its own file: the inner instance belongs to the plugin manager.
type plugin struct {
	next pluginapi.Store

	mu   sync.Mutex
	file *os.File // nil: log at debug level instead
}

// record is one traced call. It never carries the value itself.
type record struct {
	Time           string  `json:"time"`
	Op             string  `json:"op"`
	Device         string  `json:"device"`
	LocalPublicKey string  `json:"local_public_key,omitempty"`
	PeerPublicKey  string  `json:"peer_public_key,omitempty"`
	Key            string  `json:"key"`
	ValueLen       int     `json:"value_len"`
	DurationMS     float64 `json:"duration_ms"`
	OK             bool    `json:"ok"`
	Error          string  `json:"error,omitempty"`
}

// New builds a trace instance from its config, resolving next through stores.
func New(_ context.Context, config pluginapi.PluginConfig, stores pluginapi.PluginStores) (pluginapi.Store, error) {
	cfg := pluginconfig.NewConfig(config)

	// "next" and "file" are this plugin's own choice of field names; the core reserves none.
	name, err := cfg.GetStringRequired(configKeyNext)
	if err != nil {
		return nil, err
	}
	next, err := stores.Lookup(name)
	if err != nil {
		return nil, err
	}

	p := &plugin{next: next}
	if path, ok := cfg.GetString(configKeyFile); ok && path != "" {
		// Local file I/O only; the factory rule forbids network I/O, not this.
		p.file, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *plugin) Get(ctx context.Context, req pluginapi.PluginRequest) (string, error) {
	start := time.Now()
	value, err := p.next.Get(ctx, req)
	p.record(ctx, "get", req, len(value), start, err)
	return value, err
}

func (p *plugin) Set(ctx context.Context, req pluginapi.PluginRequest) error {
	start := time.Now()
	err := p.next.Set(ctx, req)
	p.record(ctx, "set", req, len(req.Value), start, err)
	return err
}

// Close closes the trace file. Safe to call more than once.
func (p *plugin) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.file == nil {
		return nil
	}
	err := p.file.Close()
	p.file = nil
	return err
}

func (p *plugin) record(ctx context.Context, op string, req pluginapi.PluginRequest, valueLen int, start time.Time, err error) {
	r := record{
		Time:       start.Format(time.RFC3339),
		Op:         op,
		Device:     req.Device,
		Key:        req.Key,
		ValueLen:   valueLen,
		DurationMS: float64(time.Since(start).Microseconds()) / 1000,
		OK:         err == nil,
	}
	if req.LocalPublicKey != (pluginapi.Key{}) {
		r.LocalPublicKey = req.LocalPublicKey.String()
	}
	if req.PeerPublicKey != (pluginapi.Key{}) {
		r.PeerPublicKey = req.PeerPublicKey.String()
	}
	if err != nil {
		r.Error = err.Error()
	}

	logger := zerolog.Ctx(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.file == nil {
		logger.Debug().
			Str("op", r.Op).
			Str("device", r.Device).
			Str("local_public_key", r.LocalPublicKey).
			Str("peer_public_key", r.PeerPublicKey).
			Str("key", r.Key).
			Int("value_len", r.ValueLen).
			Float64("duration_ms", r.DurationMS).
			Bool("ok", r.OK).
			Str("error", r.Error).
			Msg("trace")
		return
	}

	line, merr := json.Marshal(r)
	if merr != nil {
		logger.Warn().Err(merr).Msg("trace: encode record")
		return
	}
	if _, werr := p.file.Write(append(line, '\n')); werr != nil {
		logger.Warn().Err(werr).Msg("trace: write file")
	}
}
