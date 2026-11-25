package configfile

import "sync"

type Event struct {
	Component string
	Snapshot  Snapshot
}

type Subscription struct {
	Component string
	C         <-chan Event
	cancel    func()
}

type Hub struct {
	mu          sync.Mutex
	nextID      int
	subscribers map[int]subscriber
}

type subscriber struct {
	component string
	ch        chan Event
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[int]subscriber)}
}

func (hub *Hub) Subscribe(component string, buffer int) Subscription {
	if buffer < 1 {
		buffer = 1
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.nextID++
	id := hub.nextID
	ch := make(chan Event, buffer)
	hub.subscribers[id] = subscriber{component: component, ch: ch}
	return Subscription{
		Component: component,
		C:         ch,
		cancel: func() {
			hub.mu.Lock()
			defer hub.mu.Unlock()
			if sub, exists := hub.subscribers[id]; exists {
				delete(hub.subscribers, id)
				close(sub.ch)
			}
		},
	}
}

func (subscription Subscription) Cancel() {
	if subscription.cancel != nil {
		subscription.cancel()
	}
}

func (hub *Hub) Publish(snapshot Snapshot) {
	hub.mu.Lock()
	subscribers := make([]subscriber, 0, len(hub.subscribers))
	for _, sub := range hub.subscribers {
		subscribers = append(subscribers, sub)
	}
	hub.mu.Unlock()
	for _, sub := range subscribers {
		event := Event{Component: sub.component, Snapshot: Snapshot{Data: cloneBytes(snapshot.Data), Revision: snapshot.Revision}}
		select {
		case sub.ch <- event:
		default:
		}
	}
}
