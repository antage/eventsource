package http

import (
	"bytes"
	"container/list"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type eventMessage struct {
	id    string
	event string
	data  string
}

type eventSource struct {
	sink           chan *eventMessage
	staled         chan *consumer
	add            chan *consumer
	close          chan bool
	timeout        time.Duration
	closeOnTimeout bool

	consumers *list.List
}

// EventSource interface provides methods for sending messages and closing all connections.
type EventSource interface {
	// it should implement ServerHTTP method
	http.Handler

	// send message to all consumers
	SendMessage(data, event, id string)

	// consumers count
	ConsumersCount() int

	// close and clear all consumers
	Close()

	// set the timeout for message sends
	SetTimeout(n time.Duration)

	// set timeout behaviour
	CloseOnTimeout(b bool)
}

func prepareMessage(m *eventMessage) []byte {
	var data bytes.Buffer
	if len(m.id) > 0 {
		data.WriteString(fmt.Sprintf("id: %s\n", strings.Replace(m.id, "\n", "", -1)))
	}
	if len(m.event) > 0 {
		data.WriteString(fmt.Sprintf("event: %s\n", strings.Replace(m.event, "\n", "", -1)))
	}
	if len(m.data) > 0 {
		lines := strings.Split(m.data, "\n")
		for _, line := range lines {
			data.WriteString(fmt.Sprintf("data: %s\n", line))
		}
	}
	data.WriteString("\n")
	return data.Bytes()
}

func controlProcess(es *eventSource) {
	for {
		select {
		case em := <-es.sink:
			message := prepareMessage(em)
			for e := es.consumers.Front(); e != nil; e = e.Next() {
				c := e.Value.(*consumer)
				var closed bool

				// First check if a previous message caused an error
				select {
				case err := <-c.err:
					netErr, ok := err.(net.Error)
					if !ok || !netErr.Timeout() || es.closeOnTimeout {
						close(c.in)
						c.close <- true
						es.staled <- c
						closed = true
					}
				default:
				}

				// Only send this message if there was no error that
				// caused the consumer to get closed
				if !closed {
					select {
					case c.in <- message:
					default:
					}
				}
			}
		case <-es.close:
			close(es.sink)
			close(es.add)
			close(es.staled)
			close(es.close)
			for e := es.consumers.Front(); e != nil; e = e.Next() {
				c := e.Value.(*consumer)
				c.close <- true
			}
			es.consumers.Init()
			return
		case c := <-es.add:
			es.consumers.PushBack(c)
		case c := <-es.staled:
			toRemoveEls := make([]*list.Element, 0, 1)
			for e := es.consumers.Front(); e != nil; e = e.Next() {
				if e.Value.(*consumer) == c {
					toRemoveEls = append(toRemoveEls, e)
				}
			}
			for _, e := range toRemoveEls {
				es.consumers.Remove(e)
			}
		}
	}
}

// New creates new EventSource instance.
func New() EventSource {
	es := new(eventSource)
	es.sink = make(chan *eventMessage, 1)
	es.close = make(chan bool)
	es.staled = make(chan *consumer, 1)
	es.add = make(chan *consumer)
	es.consumers = list.New()
	es.timeout = 2 * time.Second
	es.closeOnTimeout = true
	go controlProcess(es)
	return es
}

// SetTimeout sets the write timeout for individual messages. The
// default is 2 seconds.
func (es *eventSource) SetTimeout(n time.Duration) {
	es.timeout = n
}

// CloseOnTimeout sets whether a write timeout should close the
// connection or just drop the message.
//
// If the connection gets closed on a timeout, it's the client's
// responsibility to re-establish a connection. If the connection
// doesn't get closed, messages might get sent to a potentially dead
// client.
//
// The default is true.
func (es *eventSource) CloseOnTimeout(b bool) {
	es.closeOnTimeout = b
}

func (es *eventSource) Close() {
	es.close <- true
}

// ServeHTTP implements http.Handler interface.
func (es *eventSource) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	cons, err := newConsumer(resp, es)
	if err != nil {
		log.Print("Can't create connection to a consumer: ", err)
		return
	}
	es.add <- cons
	// wait until EventSource closes all connection
	<-cons.close
}

func (es *eventSource) sendEventMessage(e *eventMessage) {
	es.sink <- e
}

func (es *eventSource) SendMessage(data, event, id string) {
	em := &eventMessage{id, event, data}
	es.sendEventMessage(em)
}

func (es *eventSource) ConsumersCount() int {
	return es.consumers.Len()
}
