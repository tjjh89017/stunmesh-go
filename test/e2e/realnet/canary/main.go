// Command canary serves a fixed random blob over HTTP for the realnet e2e
// full-tunnel check (test-only). The anchor binds it to an off-overlay dummy
// address; the subject can only reach that address through the covering
// WireGuard default route, so a successful fetch proves off-overlay traffic
// genuinely traverses the tunnel. Random content plus per-request nonce paths
// defeat any caching; the printed sha256 is compared by the report job.
//
// A POST to /ack is the subject's pair acknowledgement instead: its body is
// printed as one CANARY_ACK= line for the anchor script to find.
package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

const maxAckBytes = 256

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "canary:", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", "192.0.2.1:8080", "address to bind")
	size := flag.Int("size", 1<<20, "blob size in bytes")
	flag.Parse()

	blob := make([]byte, *size)
	if _, err := rand.Read(blob); err != nil {
		return err
	}
	sum := sha256.Sum256(blob)

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	// READY only after the listener is bound, so the harness can wait on it.
	fmt.Printf("CANARY_SHA256=%s\n", hex.EncodeToString(sum[:]))
	fmt.Println("CANARY_READY=1")

	return http.Serve(ln, newHandler(blob, os.Stdout))
}

func newHandler(blob []byte, out io.Writer) http.Handler {
	var mu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ack" {
			_, _ = w.Write(blob)
			return
		}
		// First line only, so the body can never forge a second log line.
		line, err := bufio.NewReader(io.LimitReader(r.Body, maxAckBytes)).ReadString('\n')
		if err != nil && err != io.EOF {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		_, err = fmt.Fprintf(out, "CANARY_ACK=%s\n", strings.TrimRight(line, "\r\n"))
		mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
