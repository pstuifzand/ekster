package server

import (
	"log"
)

// A MessageChan is a channel of channels
// Each connection sends a channel of bytes to a global MessageChan
// The main broker listen() loop listens on new connections on MessageChan
// New event messages are broadcast to all registered connection channels
type MessageChan chan Message

type Message struct {
	Event  string
	Object interface{}
}

// A Broker holds open client connections,
// listens for incoming events on its Notifier channel
// and broadcast event data to all registered connections
type Broker struct {
	// Events are pushed to this channel by the main UDP daemon
	Notifier chan Message

	// New client connections
	newClients chan MessageChan

	// Closed client connections
	closingClients chan MessageChan

	// Client connections registry
	clients map[MessageChan]bool
}

// Listen on different channels and act accordingly
func (broker *Broker) listen() {
	for {
		select {
		case s := <-broker.newClients:
			// A new client has connected.
			// Register their message channel
			broker.clients[s] = true
			log.Printf("Client added. %d registered clients", len(broker.clients))
		case s := <-broker.closingClients:
			// A client has detached and we want to
			// stop sending them messages.
			delete(broker.clients, s)
			log.Printf("Removed client. %d registered clients", len(broker.clients))
		case event := <-broker.Notifier:
			// We got a new event from the outside!
			// Send event to all connected clients
			for clientMessageChan := range broker.clients {
				clientMessageChan <- event
			}
		}
	}

}

// Broker factory
func NewServer() (broker *Broker) {
	// Instantiate a broker
	broker = &Broker{
		Notifier:       make(chan Message, 1),
		newClients:     make(chan MessageChan),
		closingClients: make(chan MessageChan),
		clients:        make(map[MessageChan]bool),
	}

	// Set it running - listening and broadcasting events
	go broker.listen()

	return
}
