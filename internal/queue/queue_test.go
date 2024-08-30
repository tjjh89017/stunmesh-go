package queue_test

import (
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/queue"
)

func Test_Queue_Dequeue(t *testing.T) {
	t.Parallel()

	q := queue.New[int]()
	go func() {
		for i := 0; i < 3; i++ {
			q.Enqueue(i)
		}
	}()

	for i := 0; i < 3; i++ {
		select {
		case v := <-q.Dequeue():
			if v != i {
				t.Errorf("Expected %d, got %d", i, v)
			}
		default:
			continue
		}
	}
}
