package queue_test

import (
	"sync"
	"testing"
	"time"

	"github.com/tjjh89017/stunmesh-go/internal/queue"
)

func Test_Queue_Dequeue(t *testing.T) {
	t.Parallel()

	q := queue.New[int]()
	done := make(chan bool)

	go func() {
		for i := 0; i < 3; i++ {
			q.Enqueue(i)
		}
		close(done)
	}()

	for i := 0; i < 3; i++ {
		v := <-q.Dequeue()
		if v != i {
			t.Errorf("Expected %d, got %d", i, v)
		}
	}

	<-done // Wait for enqueue goroutine to finish
}

func TestNew(t *testing.T) {
	q := queue.New[int]()

	if q == nil {
		t.Fatal("Expected queue to be created")
	}

	// Verify it's unbuffered (Len should be 0)
	if q.Len() != 0 {
		t.Errorf("Expected initial length 0, got %d", q.Len())
	}
}

func TestNewBuffered(t *testing.T) {
	size := 10
	q := queue.NewBuffered[int](size)

	if q == nil {
		t.Fatal("Expected buffered queue to be created")
	}

	if q.Len() != 0 {
		t.Errorf("Expected initial length 0, got %d", q.Len())
	}
}

func TestQueue_EnqueueDequeue(t *testing.T) {
	q := queue.NewBuffered[int](5)

	// Enqueue some values
	q.Enqueue(1)
	q.Enqueue(2)
	q.Enqueue(3)

	// Dequeue and verify
	v := <-q.Dequeue()
	if v != 1 {
		t.Errorf("Expected 1, got %d", v)
	}

	v = <-q.Dequeue()
	if v != 2 {
		t.Errorf("Expected 2, got %d", v)
	}

	v = <-q.Dequeue()
	if v != 3 {
		t.Errorf("Expected 3, got %d", v)
	}
}

func TestQueue_TryEnqueue_Success(t *testing.T) {
	q := queue.NewBuffered[int](5)

	// Should succeed on empty queue
	ok := q.TryEnqueue(1)
	if !ok {
		t.Error("Expected TryEnqueue to succeed on empty queue")
	}

	// Verify it was enqueued
	v := <-q.Dequeue()
	if v != 1 {
		t.Errorf("Expected 1, got %d", v)
	}
}

func TestQueue_TryEnqueue_Full(t *testing.T) {
	q := queue.NewBuffered[int](2)

	// Fill the queue
	ok := q.TryEnqueue(1)
	if !ok {
		t.Fatal("Expected first TryEnqueue to succeed")
	}

	ok = q.TryEnqueue(2)
	if !ok {
		t.Fatal("Expected second TryEnqueue to succeed")
	}

	// Queue is now full, should fail
	ok = q.TryEnqueue(3)
	if ok {
		t.Error("Expected TryEnqueue to fail on full queue")
	}

	// Dequeue one item
	<-q.Dequeue()

	// Should succeed now
	ok = q.TryEnqueue(3)
	if !ok {
		t.Error("Expected TryEnqueue to succeed after dequeue")
	}
}

func TestQueue_Len(t *testing.T) {
	q := queue.NewBuffered[int](10)

	if q.Len() != 0 {
		t.Errorf("Expected initial length 0, got %d", q.Len())
	}

	q.Enqueue(1)
	if q.Len() != 1 {
		t.Errorf("Expected length 1, got %d", q.Len())
	}

	q.Enqueue(2)
	if q.Len() != 2 {
		t.Errorf("Expected length 2, got %d", q.Len())
	}

	<-q.Dequeue()
	if q.Len() != 1 {
		t.Errorf("Expected length 1 after dequeue, got %d", q.Len())
	}
}

func TestQueue_ConcurrentEnqueue(t *testing.T) {
	bufferSize := 300
	q := queue.NewBuffered[int](bufferSize)
	count := 50
	var wg sync.WaitGroup

	// Start multiple goroutines enqueueing
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < count; j++ {
				q.Enqueue(j)
			}
		}()
	}

	wg.Wait()

	// Verify total count
	expectedTotal := 5 * count
	if q.Len() != expectedTotal {
		t.Errorf("Expected queue length %d, got %d", expectedTotal, q.Len())
	}
}

func TestQueue_ConcurrentDequeue(t *testing.T) {
	count := 50
	bufferSize := count * 2
	q := queue.NewBuffered[int](bufferSize)
	results := make(chan int, count*2)

	// Enqueue items
	for i := 0; i < count*2; i++ {
		q.Enqueue(i)
	}

	var wg sync.WaitGroup

	// Start multiple goroutines dequeueing
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < count; j++ {
				v := <-q.Dequeue()
				results <- v
			}
		}()
	}

	wg.Wait()
	close(results)

	// Verify we got all items
	resultCount := 0
	for range results {
		resultCount++
	}

	if resultCount != count*2 {
		t.Errorf("Expected %d results, got %d", count*2, resultCount)
	}
}

func TestQueue_CloseWhileBlocked(t *testing.T) {
	q := queue.NewBuffered[int](5)
	done := make(chan bool)
	receivedValue := make(chan int, 1)

	// Start goroutine that will block on dequeue
	go func() {
		v, ok := <-q.Dequeue()
		if !ok {
			// Channel was closed, v will be zero value
			receivedValue <- -1
		} else {
			receivedValue <- v
		}
		done <- true
	}()

	// Give goroutine time to block
	time.Sleep(100 * time.Millisecond)

	// Close the queue
	q.Close()

	// Wait for goroutine to finish
	select {
	case <-done:
		// Verify we got the closed channel signal
		v := <-receivedValue
		if v != -1 {
			t.Errorf("Expected -1 (closed channel), got %d", v)
		}
	case <-time.After(1 * time.Second):
		t.Error("Goroutine did not unblock after queue close")
	}
}

func TestQueue_EnqueueAfterClose(t *testing.T) {
	q := queue.NewBuffered[int](5)

	// Close the queue
	q.Close()

	// Attempting to enqueue should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when enqueueing to closed queue")
		}
	}()

	q.Enqueue(1)
}

func TestQueue_TypeSafety(t *testing.T) {
	// Test with string type
	sq := queue.NewBuffered[string](5)
	sq.Enqueue("hello")
	sq.Enqueue("world")

	v1 := <-sq.Dequeue()
	if v1 != "hello" {
		t.Errorf("Expected 'hello', got %s", v1)
	}

	v2 := <-sq.Dequeue()
	if v2 != "world" {
		t.Errorf("Expected 'world', got %s", v2)
	}

	// Test with struct type
	type TestStruct struct {
		ID   int
		Name string
	}

	tq := queue.NewBuffered[TestStruct](5)
	tq.Enqueue(TestStruct{ID: 1, Name: "first"})
	tq.Enqueue(TestStruct{ID: 2, Name: "second"})

	s1 := <-tq.Dequeue()
	if s1.ID != 1 || s1.Name != "first" {
		t.Errorf("Expected {1, 'first'}, got %+v", s1)
	}

	s2 := <-tq.Dequeue()
	if s2.ID != 2 || s2.Name != "second" {
		t.Errorf("Expected {2, 'second'}, got %+v", s2)
	}
}

func TestQueue_Constants(t *testing.T) {
	// Verify constants are reasonable
	if queue.TriggerQueueSize <= 0 {
		t.Errorf("TriggerQueueSize should be positive, got %d", queue.TriggerQueueSize)
	}

	if queue.PeerQueueSize <= 0 {
		t.Errorf("PeerQueueSize should be positive, got %d", queue.PeerQueueSize)
	}

	if queue.PeerQueueSize < queue.TriggerQueueSize {
		t.Error("PeerQueueSize should be larger than TriggerQueueSize")
	}

	t.Logf("TriggerQueueSize: %d", queue.TriggerQueueSize)
	t.Logf("PeerQueueSize: %d", queue.PeerQueueSize)
}
