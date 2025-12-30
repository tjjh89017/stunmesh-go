package queue

const (
	TriggerQueueSize = 5   // Buffer size for simple trigger queues
	PeerQueueSize    = 100 // Buffer size for peer-specific queues
)

type Queue[T any] struct {
	stack chan T
}

func New[T any]() *Queue[T] {
	return &Queue[T]{
		stack: make(chan T),
	}
}

func NewBuffered[T any](size int) *Queue[T] {
	return &Queue[T]{
		stack: make(chan T, size),
	}
}

func (q *Queue[T]) Enqueue(entity T) {
	q.stack <- entity
}

// TryEnqueue attempts to enqueue an entity without blocking
// Returns true if successful, false if queue is full
func (q *Queue[T]) TryEnqueue(entity T) bool {
	select {
	case q.stack <- entity:
		return true
	default:
		return false
	}
}

func (q *Queue[T]) Dequeue() <-chan T {
	return q.stack
}

func (q *Queue[T]) Close() {
	close(q.stack)
}
