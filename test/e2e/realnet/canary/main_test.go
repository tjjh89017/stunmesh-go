package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerAck(t *testing.T) {
	var out bytes.Buffer
	srv := httptest.NewServer(newHandler([]byte("blob"), &out))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/ack", "text/plain", strings.NewReader("pair=windows attempt=2\nCANARY_ACK=forged\n"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	if got, want := out.String(), "CANARY_ACK=pair=windows attempt=2\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestHandlerBlob(t *testing.T) {
	var out bytes.Buffer
	srv := httptest.NewServer(newHandler([]byte("blob"), &out))
	defer srv.Close()

	for _, req := range []struct{ method, path string }{
		{http.MethodGet, "/nonce"},
		{http.MethodGet, "/ack"},
		{http.MethodPost, "/other"},
	} {
		r, err := http.NewRequest(req.method, srv.URL+req.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "blob" {
			t.Errorf("%s %s: body = %q, want the blob", req.method, req.path, body)
		}
	}
	if out.Len() != 0 {
		t.Errorf("non-ack requests wrote %q", out.String())
	}
}
