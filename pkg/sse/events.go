/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package sse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// A MessageChan is a channel of channels
// Each connection sends a channel of bytes to a global MessageChan
// The main broker listen() loop listens on new connections on MessageChan
// New event messages are broadcast to all registered connection channels
type MessageChan chan Message

// Message is a message.
type Message struct {
	Event  string
	Data   string
	Object interface{}
}

type pingMessage struct {
	PingCount int `json:"ping"`
}

// Broker holds open client connections,
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
	ticker := time.NewTicker(10 * time.Second)
	pingCount := 0
	for {
		select {
		case <-ticker.C:
			broker.Notifier <- Message{
				Event:  "ping",
				Object: pingMessage{PingCount: pingCount},
			}
			pingCount++
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

// NewBroker creates a Broker.
func NewBroker() (broker *Broker) {
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

// CloseClient closes the client channel
func (broker *Broker) CloseClient(ch MessageChan) {
	broker.closingClients <- ch
}

// StartConnection starts a SSE connection, based on an existing HTTP connection.
func StartConnection(broker *Broker) (MessageChan, error) {
	// Each connection registers its own message channel with the Broker's connections registry
	messageChan := make(MessageChan)

	// Signal the broker that we have a new connection
	broker.newClients <- messageChan

	return messageChan, nil
}

type welcomeMessage struct {
	Version string `json:"version"`
}

// WriteMessages writes SSE formatted messages to the writer
func WriteMessages(w http.ResponseWriter, messageChan chan Message) error {
	// Make sure that the writer supports flushing.
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming unsupported")
	}

	// Set the headers related to event streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var welcomeMsg welcomeMessage
	welcomeMsg.Version = "1.0.0"
	encoded, err := json.Marshal(&welcomeMsg)
	if err != nil {
		return errors.Wrap(err, "could not encode welcome message")
	}

	messageID := 1

	_, err = fmt.Fprintf(w, "event: started\r\nid: %d\r\ndata: %s\r\n\r\n", messageID, encoded)
	if err != nil {
		return err
	}

	messageID++
	flusher.Flush()

	// block waiting or messages broadcast on this connection's messageChan
	for message := range messageChan {
		// Write to the ResponseWriter, Server Sent Events compatible
		output, err := json.Marshal(message.Object)
		if err != nil {
			return errors.Wrap(err, "could not marshal message data")
		}

		_, err = fmt.Fprintf(w, "event: %s\r\nid: %d\r\ndata: %s\r\n\r\n", message.Event, messageID, output)
		if err != nil {
			return errors.Wrap(err, "could not write message")
		}

		messageID++

		flusher.Flush()
	}

	return nil
}

// Reader returns a channel that contains parsed SSE messages.
func Reader(body io.ReadCloser, ch MessageChan) error {
	r := bufio.NewScanner(body)
	var msg Message

	for r.Scan() {
		line := r.Text()
		if line == "" {
			ch <- msg
			msg = Message{}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			line = line[len("event: "):]
			msg.Event = line
		}
		if strings.HasPrefix(line, "data: ") {
			line = line[len("data: "):]
			msg.Data = line
		}
	}
	if err := r.Err(); err != nil {
		return errors.Wrap(err, "could not scan lines from sse events")
	}
	return nil
}
