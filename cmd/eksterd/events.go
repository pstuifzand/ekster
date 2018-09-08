package main

import (
	"encoding/json"
	"fmt"
	"net"

	"p83.nl/go/ekster/pkg/microsub"
)

type Consumer struct {
	conn   net.Conn
	output chan microsub.Message
}

func newConsumer(conn net.Conn) *Consumer {
	cons := &Consumer{conn, make(chan microsub.Message)}

	fmt.Fprint(conn, "HTTP/1.0 200 OK\r\n")
	fmt.Fprint(conn, "Content-Type: text/event-stream\r\n")
	fmt.Fprint(conn, "Access-Control-Allow-Origin: *\r\n")
	fmt.Fprint(conn, "\r\n")

	go func() {
		for msg := range cons.output {
			fmt.Fprint(conn, `event: ping`)
			fmt.Fprint(conn, "\r\n")
			fmt.Fprint(conn, `data:`)
			json.NewEncoder(conn).Encode(msg)
			fmt.Fprint(conn, "\r\n")
			fmt.Fprint(conn, "\r\n")
		}
		conn.Close()
	}()

	return cons
}

func (cons *Consumer) WriteMessage(evt microsub.Event) {
	cons.output <- evt.Msg
}
