package resourcepool

import (
	"container/heap"
	"context"
)

type jobState uint8

const (
	jobQueued jobState = iota
	jobRunning
)

type jobEntry struct {
	job              Job
	ctx              context.Context
	cancel           context.CancelCauseFunc
	stopContextWatch func() bool
	future           *Future
	sequence         uint64
	index            int
	state            jobState
	cancelRequested  bool
}

type priorityQueue []*jobEntry

func (queue priorityQueue) Len() int { return len(queue) }

func (queue priorityQueue) Less(left, right int) bool {
	if queue[left].job.Priority != queue[right].job.Priority {
		return queue[left].job.Priority > queue[right].job.Priority
	}
	return queue[left].sequence < queue[right].sequence
}

func (queue priorityQueue) Swap(left, right int) {
	queue[left], queue[right] = queue[right], queue[left]
	queue[left].index = left
	queue[right].index = right
}

func (queue *priorityQueue) Push(value any) {
	entry := value.(*jobEntry)
	entry.index = len(*queue)
	*queue = append(*queue, entry)
}

func (queue *priorityQueue) Pop() any {
	old := *queue
	last := len(old) - 1
	entry := old[last]
	old[last] = nil
	entry.index = -1
	*queue = old[:last]
	return entry
}

func removeEntry(queue *priorityQueue, entry *jobEntry) {
	if entry.index >= 0 && entry.index < queue.Len() {
		heap.Remove(queue, entry.index)
	}
}

func popOldest(queue *priorityQueue) *jobEntry {
	oldestIndex := 0
	for index := 1; index < queue.Len(); index++ {
		if (*queue)[index].sequence < (*queue)[oldestIndex].sequence {
			oldestIndex = index
		}
	}
	return heap.Remove(queue, oldestIndex).(*jobEntry)
}
