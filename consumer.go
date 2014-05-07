package http

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

type consumer struct {
	conn   net.Conn
	es     *eventSource
	in     chan []byte
	staled bool
}

func newConsumer(resp http.ResponseWriter, req *http.Request, es *eventSource) (*consumer, error) {
	conn, _, err := resp.(http.Hijacker).Hijack()
	if err != nil {
		return nil, err
	}

	consumer := &consumer{
		conn:   conn,
		es:     es,
		in:     make(chan []byte, 10),
		staled: false,
	}

	conn.Write([]byte("HTTP/1.1 200 OK\nContent-Type: text/event-stream\n"))

	if es.customHeadersFunc != nil {
		for _, header := range es.customHeadersFunc(req) {
			conn.Write(header)
			conn.Write([]byte("\n"))
		}
	}

	_, err = conn.Write([]byte(fmt.Sprintf("retry: %d\n\n", es.retry/time.Millisecond)))

	if err != nil {
		conn.Close()
		return nil, err
	}

	go func() {
		idleTimer := time.NewTimer(es.idleTimeout)
		defer idleTimer.Stop()
		for {
			select {
			case message, open := <-consumer.in:
				if !open {
					consumer.conn.Close()
					return
				}
				conn.SetWriteDeadline(time.Now().Add(consumer.es.timeout))
				_, err := conn.Write(message)
				if err != nil {
					netErr, ok := err.(net.Error)
					if !ok || !netErr.Timeout() || consumer.es.closeOnTimeout {
						consumer.staled = true
						consumer.conn.Close()
						consumer.es.staled <- consumer
						return
					}
				}
				idleTimer.Reset(es.idleTimeout)
			case <-idleTimer.C:
				consumer.conn.Close()
				consumer.es.staled <- consumer
				return
			}
		}
	}()

	return consumer, nil
}
