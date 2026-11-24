package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

var errNotFound = errors.New("value not stored")

func signalKeyHash(seed string) string {
	sum := sha1.Sum([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// signal stores put, or looks for a value starting with has and prints it,
// under the key derived from seed. The first endpoint that answers decides; a
// failed request moves to the next.
func signal(ctx context.Context, seed string, endpoints []string, put, has string) error {
	if (put == "") == (has == "") {
		return errors.New("need exactly one of -signal-put and -signal-has")
	}
	if len(endpoints) == 0 {
		return errors.New("no -signal-endpoints")
	}
	key := signalKeyHash(seed)
	var errs []error
	for _, ep := range endpoints {
		url := strings.TrimRight(ep, "/") + "/key/" + key
		var err error
		if put != "" {
			err = signalPost(ctx, url, put)
		} else {
			var body []byte
			body, err = signalGet(ctx, url)
			if err == nil {
				if v, ok := signalFind(body, has); ok {
					logf(os.Stdout, "%s\n", v)
					return nil
				}
				return errNotFound
			}
		}
		if err == nil {
			return nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", ep, err))
	}
	return errors.Join(errs...)
}

func signalPost(ctx context.Context, url, value string) error {
	body, err := json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString([]byte(value))})
	if err != nil {
		return err
	}
	_, err = signalDo(ctx, http.MethodPost, url, body)
	return err
}

func signalGet(ctx context.Context, url string) ([]byte, error) {
	return signalDo(ctx, http.MethodGet, url, nil)
}

func signalDo(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(data))
	}
	return data, nil
}

// signalFind returns the first of the proxy's newline-delimited JSON values
// that starts with prefix. Anything that does not decode is ignored.
func signalFind(body []byte, prefix string) (string, bool) {
	for _, line := range bytes.Split(body, []byte("\n")) {
		var v struct {
			Data string `json:"data"`
		}
		if json.Unmarshal(line, &v) != nil {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(v.Data)
		if err == nil && strings.HasPrefix(string(raw), prefix) {
			return string(raw), true
		}
	}
	return "", false
}
