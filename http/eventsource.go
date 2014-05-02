package http

import (
	"bytes"
	"container/list"
	"fmt"
	"log"
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
	customHeadersFunc func(*http.Request) [][]byte

	sink           chan *eventMessage
	staled         chan *consumer
	add            chan *consumer
	close          chan bool
	retry          int
	timeout        time.Duration
	closeOnTimeout bool

	consumers *list.List
}

type Settings struct {
	// Sets the delay between a connection loss and the client attempting to
	// reconnect. This is given in milliseconds.
	Retry int

	// SetTimeout sets the write timeout for individual messages. The
	// default is 2 seconds.
	Timeout time.Duration

	// CloseOnTimeout sets whether a write timeout should close the
	// connection or just drop the message.
	//
	// If the connection gets closed on a timeout, it's the client's
	// responsibility to re-establish a connection. If the connection
	// doesn't get closed, messages might get sent to a potentially dead
	// client.
	//
	// The default is true.
	CloseOnTimeout bool
}

func DefaultSettings() *Settings {
	return &Settings{
		Timeout:        2 * time.Second,
		CloseOnTimeout: true,
		Retry:          3000,
	}
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

				// Only send this message if the consumer isn't staled
				if !c.staled {
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
				close(c.in)
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
			close(c.in)
		}
	}
}

// New creates new EventSource instance.
func New(settings *Settings, customHeadersFunc func(*http.Request) [][]byte) EventSource {
	if settings == nil {
		settings = DefaultSettings()
	}

	es := new(eventSource)
	es.customHeadersFunc = customHeadersFunc
	es.sink = make(chan *eventMessage, 1)
	es.close = make(chan bool)
	es.staled = make(chan *consumer, 1)
	es.add = make(chan *consumer)
	es.consumers = list.New()
	es.retry = settings.Retry
	es.timeout = settings.Timeout
	es.closeOnTimeout = settings.CloseOnTimeout
	go controlProcess(es)
	return es
}

func (es *eventSource) Close() {
	es.close <- true
}

// ServeHTTP implements http.Handler interface.
func (es *eventSource) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	cons, err := newConsumer(resp, req, es)
	if err != nil {
		log.Print("Can't create connection to a consumer: ", err)
		return
	}
	es.add <- cons
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
