package http

import (
    "net"
    "net/http"
)

type consumer struct {
    conn  net.Conn
    close chan bool
}

func newConsumer(resp http.ResponseWriter) (*consumer, error) {
    conn, _, err := resp.(http.Hijacker).Hijack()
    if err != nil {
        return nil, err
    }
    conn.Write([]byte("HTTP/1.1 200 OK\nContent-Type: text/event-stream\nX-Accel-Buffering: no\n\n"))

    return &consumer{conn, make(chan bool)}, nil
}
