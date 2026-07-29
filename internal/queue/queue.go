package queue

const (
	TriggerQueueSize = 5   // Buffer size for simple trigger queues
	PeerQueueSize    = 100 // Buffer size for peer-specific queues
)

type Queue[T any] struct {
	items chan T
}

func NewBuffered[T any](size int) *Queue[T] {
	return &Queue[T]{
		items: make(chan T, size),
	}
}

func (q *Queue[T]) Enqueue(entity T) {
	q.items <- entity
}

// TryEnqueue attempts to enqueue an entity without blocking
// Returns true if successful, false if queue is full
func (q *Queue[T]) TryEnqueue(entity T) bool {
	select {
	case q.items <- entity:
		return true
	default:
		return false
	}
}

func (q *Queue[T]) Dequeue() <-chan T {
	return q.items
}

func (q *Queue[T]) Len() int {
	return len(q.items)
}

func (q *Queue[T]) Close() {
	close(q.items)
}
