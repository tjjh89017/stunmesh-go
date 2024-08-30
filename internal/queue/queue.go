package queue

type Queue[T any] struct {
	stack chan T
}

func New[T any]() *Queue[T] {
	return &Queue[T]{
		stack: make(chan T),
	}
}

func (q *Queue[T]) Enqueue(entity T) {
	q.stack <- entity
}

func (q *Queue[T]) Dequeue() <-chan T {
	return q.stack
}

func (q *Queue[T]) Close() {
	close(q.stack)
}
