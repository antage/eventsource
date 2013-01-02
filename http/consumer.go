package http

import (
	"net"
	"net/http"
	"time"
)

type consumer struct {
	conn  net.Conn
	es    *eventSource
	in    chan []byte
	err   chan error
	close chan bool
}

func newConsumer(resp http.ResponseWriter, es *eventSource) (*consumer, error) {
	conn, _, err := resp.(http.Hijacker).Hijack()
	if err != nil {
		return nil, err
	}

	consumer := &consumer{
		conn:  conn,
		es:    es,
		in:    make(chan []byte, 10),
		err:   make(chan error),
		close: make(chan bool),
	}

	consumer.in <- []byte("HTTP/1.1 200 OK\nContent-Type: text/event-stream\nX-Accel-Buffering: no\n\n")

	go func() {
		for message := range consumer.in {
			conn.SetWriteDeadline(time.Now().Add(consumer.es.timeout))
			_, err := conn.Write(message)
			if err != nil {
				consumer.err <- err
			}
		}

		close(consumer.err)
	}()

	return consumer, nil
}
