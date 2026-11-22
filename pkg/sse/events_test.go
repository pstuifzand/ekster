package sse

import (
	"testing"
	"time"
)

func TestUserScopedDelivery(t *testing.T) {
	b := NewBroker()
	one, _ := StartUserConnection(b, 1)
	two, _ := StartUserConnection(b, 2)
	defer b.CloseClient(one)
	defer b.CloseClient(two)
	id := 1
	b.Notifier <- Message{Event: "private", UserID: &id}
	select {
	case m := <-one:
		if m.Event != "private" {
			t.Fatal(m)
		}
	case <-time.After(time.Second):
		t.Fatal("missing targeted event")
	}
	// A later broadcast is a barrier: the other user must see it first.
	b.Notifier <- Message{Event: "broadcast"}
	results := make(chan Message, 2)
	go func() { results <- <-one }()
	go func() { results <- <-two }()
	for i := 0; i < 2; i++ {
		select {
		case m := <-results:
			if m.Event != "broadcast" {
				t.Fatalf("unexpected delivery: %#v", m)
			}
		case <-time.After(time.Second):
			t.Fatal("missing broadcast")
		}
	}
}
