/*
Package eventsource provides server-sent events for net/http server.

Example:

		package main

		import (
		    "eventsource"
		    "log"
		    "net/http"
		    "strconv"
		    "time"
		)

		func main() {
		    es := eventsource.New()
		    defer es.Close()
		    http.Handle("/events", es)
		    go func() {
		    	id := 1
		        for {
		            es.SendMessage("tick", "tick-event", strconv.Itoa(id))
					id++
		            time.Sleep(2 * time.Second)
		        }
		    }()
		    log.Fatal(http.ListenAndServe(":8080", nil))
		}
*/
package eventsource

import (
	"bufio"
	"container/list"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
)

type consumer struct {
	conn  net.Conn
	bufrw *bufio.ReadWriter
}

type eventMessage struct {
	id    string
	event string
	data  string
}

type eventSource struct {
	sink   chan *eventMessage
	staled chan *consumer
	add    chan *consumer
	close  chan interface{}

	consumers *list.List
}

// EventSource interface provides methods for sending messages and closing all connections.
type EventSource interface {
	http.Handler
	SendMessage(data, event, id string)
	Close()
}

func prepareMessage(m *eventMessage) []byte {
	data := make([]byte, 0)
	if len((*m).id) > 0 {
		data = append(data, []byte(fmt.Sprintf("id: %s\n", strings.Replace((*m).id, "\n", "", -1)))...)
	}
	if len((*m).event) > 0 {
		data = append(data, []byte(fmt.Sprintf("event: %s\n", strings.Replace((*m).event, "\n", "", -1)))...)
	}
	if len((*m).data) > 0 {
		lines := strings.Split((*m).data, "\n")
		for _, line := range lines {
			data = append(data, []byte(fmt.Sprintf("data: %s\n", line))...)
		}
	}
	data = append(data, []byte("\n")...)
	return data
}

func controlProcess(es *eventSource) {
	for {
		select {
		case em := <-(*es).sink:
			message := prepareMessage(em)
			for e := (*es).consumers.Front(); e != nil; e = e.Next() {
				go func(c *consumer, message []byte) {
					_, err1 := (*c).bufrw.Write(message)
					err2 := (*c).bufrw.Flush()
					if err1 != nil || err2 != nil {
						err := (*c).conn.Close()
						if err != nil {
							log.Print("EventSource can't close connection to a consumer: ", err)
						}
						(*es).staled <- c
					}
				}(e.Value.(*consumer), message)
			}
		case <-(*es).close:
			close((*es).sink)
			close((*es).add)
			close((*es).staled)
			close((*es).close)
			for e := (*es).consumers.Front(); e != nil; e = e.Next() {
				c := e.Value.(*consumer)
				(*c).conn.Close()
			}
			(*es).consumers.Init()
			return
		case c := <-(*es).add:
			(*es).consumers.PushBack(c)
		case c := <-(*es).staled:
			toRemoveEls := make([]*list.Element, 0)
			for e := (*es).consumers.Front(); e != nil; e = e.Next() {
				if e.Value.(*consumer) == c {
					toRemoveEls = append(toRemoveEls, e)
				}
			}
			for _, e := range toRemoveEls {
				(*es).consumers.Remove(e)
			}
		}
	}
}

// New creates new EventSource instance.
func New() EventSource {
	es := new(eventSource)
	(*es).sink = make(chan *eventMessage, 256)
	(*es).close = make(chan interface{})
	(*es).staled = make(chan *consumer)
	(*es).add = make(chan *consumer)
	(*es).consumers = list.New()
	go controlProcess(es)
	return es
}

func (es *eventSource) Close() {
	(*es).close <- true
}

// ServeHTTP implements http.Handler interface.
func (es *eventSource) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	conn, bufrw, err := resp.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("EventSource can't create connection to a consumer: ", err)
		return
	}
	bufrw.Write([]byte("HTTP/1.1 200 OK\n"))
	bufrw.Write([]byte("Content-Type: text/event-stream\n"))
	bufrw.Write([]byte("\n"))
	bufrw.Flush()

	(*es).add <- &consumer{conn, bufrw}
}

func (es *eventSource) sendEventMessage(e *eventMessage) {
	(*es).sink <- e
}

func (es *eventSource) SendMessage(data, event, id string) {
	em := &eventMessage{id, event, data}
	(*es).sendEventMessage(em)
}
