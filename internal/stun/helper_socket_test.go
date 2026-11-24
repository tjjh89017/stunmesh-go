//go:build !windows

package stun

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	stun "github.com/pion/stun/v3"
)

func TestDecodeStunPacket_ShortPacket(t *testing.T) {
	buf := []byte{1, 2, 3, 4, 5, 6, 7}

	_, err := decodeStunPacket(buf, 8)
	if !errors.Is(err, errShortPacket) {
		t.Fatalf("expected errShortPacket, got %v", err)
	}
}

func TestDecodeStunPacket_DecodeFailure(t *testing.T) {
	const payloadOff = 32
	// Long enough to pass the short-packet guard, far short of STUN's 20-byte
	// message header.
	buf := append(make([]byte, payloadOff), 0xff, 0xff, 0xff, 0xff)

	_, err := decodeStunPacket(buf, payloadOff)
	if err == nil {
		t.Fatal("expected a STUN decode error")
	}
	if errors.Is(err, errShortPacket) {
		t.Errorf("expected a decode error distinct from errShortPacket, got %v", err)
	}
}

func TestStun_Read_ReceivesDecodedMessage(t *testing.T) {
	s := &Stun{packetChan: make(chan *stun.Message, 1)}
	want := &stun.Message{}
	s.packetChan <- want

	got, err := s.Read(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Read returned %p, want the exact message sent on packetChan (%p)", got, want)
	}
}

func buildMessage(t *testing.T, setters ...stun.Setter) *stun.Message {
	t.Helper()
	m, err := stun.Build(setters...)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestPump_KeepsReadingAcrossHandOffs drives the reader loop both platforms
// share with a fake source: poll timeouts, undecodable packets and several
// messages in a row, as a pcap handle sees them while the resolver walks its
// server list.
func TestPump_KeepsReadingAcrossHandOffs(t *testing.T) {
	const payloadOff = 8
	msgs := []*stun.Message{
		buildMessage(t, stun.TransactionID, stun.BindingSuccess),
		buildMessage(t, stun.TransactionID, stun.BindingSuccess),
	}

	steps := []func() ([]byte, error){
		func() ([]byte, error) { return nil, context.DeadlineExceeded },
		func() ([]byte, error) { return append(make([]byte, payloadOff), msgs[0].Raw...), nil },
		func() ([]byte, error) { return append(make([]byte, payloadOff), 0xff, 0xff), nil },
		func() ([]byte, error) { return []byte{1, 2}, nil },
		func() ([]byte, error) { return nil, context.DeadlineExceeded },
		func() ([]byte, error) { return append(make([]byte, payloadOff), msgs[1].Raw...), nil },
	}
	parked := make(chan struct{})
	read := func() ([]byte, error) {
		if len(steps) == 0 {
			<-parked
			return nil, errReaderClosed
		}
		step := steps[0]
		steps = steps[1:]
		return step()
	}

	s := &Stun{packetChan: make(chan *stun.Message), done: make(chan struct{})}
	exited := make(chan struct{})
	go func() {
		s.pump(t.Context(), "fake", read, payloadOff)
		close(exited)
	}()

	for i, want := range msgs {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		got, err := s.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if got.TransactionID != want.TransactionID {
			t.Fatalf("message %d: got transaction %x, want %x", i, got.TransactionID, want.TransactionID)
		}
	}

	close(parked)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("pump did not return on errReaderClosed")
	}
}

func TestPump_ReturnsOnDone(t *testing.T) {
	s := &Stun{packetChan: make(chan *stun.Message), done: make(chan struct{})}
	m := buildMessage(t, stun.TransactionID, stun.BindingSuccess)
	read := func() ([]byte, error) { return m.Raw, nil }

	exited := make(chan struct{})
	go func() {
		s.pump(t.Context(), "fake", read, 0)
		close(exited)
	}()

	// The reader is now parked on a hand-off nobody takes.
	close(s.done)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("pump did not return after done was closed")
	}
}

func TestPump_FailingSourceDoesNotSpin(t *testing.T) {
	const period = 300 * time.Millisecond
	s := &Stun{packetChan: make(chan *stun.Message), done: make(chan struct{})}
	var reads atomic.Int64
	read := func() ([]byte, error) {
		reads.Add(1)
		return nil, errors.New("device not configured")
	}

	exited := make(chan struct{})
	go func() {
		s.pump(t.Context(), "fake", read, 0)
		close(exited)
	}()

	time.Sleep(period)
	stopAt := time.Now()
	close(s.done)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("pump did not return after done was closed")
	}
	if d := time.Since(stopAt); d > readRetryDelay+200*time.Millisecond {
		t.Errorf("pump took %v to see done", d)
	}
	if n, limit := reads.Load(), int64(period/readRetryDelay)+2; n > limit {
		t.Errorf("failing source read %d times in %v, want at most %d", n, period, limit)
	}
}

func TestPump_PollTimeoutKeepsReader(t *testing.T) {
	s := &Stun{packetChan: make(chan *stun.Message), done: make(chan struct{})}
	m := buildMessage(t, stun.TransactionID, stun.BindingSuccess)
	var calls atomic.Int64
	read := func() ([]byte, error) {
		if calls.Add(1) <= 3 {
			return nil, context.DeadlineExceeded
		}
		return m.Raw, nil
	}
	go s.pump(t.Context(), "fake", read, 0)
	t.Cleanup(func() { close(s.done) })

	ctx, cancel := context.WithTimeout(t.Context(), readRetryDelay)
	defer cancel()
	got, err := s.Read(ctx)
	if err != nil {
		t.Fatalf("reply after poll timeouts: %v", err)
	}
	if got.TransactionID != m.TransactionID {
		t.Fatal("wrong message")
	}
}
