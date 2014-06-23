package eventsource

import (
	"bytes"
	"container/list"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type eventMessage struct {
	id    string
	event string
	data  string
}

type retryMessage struct {
	retry time.Duration
}

type eventSource struct {
	customHeadersFunc func(*http.Request) [][]byte

	sink           chan message
	staled         chan *consumer
	add            chan *consumer
	close          chan bool
	idleTimeout    time.Duration
	retry          time.Duration
	timeout        time.Duration
	closeOnTimeout bool

	consumersLock sync.RWMutex
	consumers     *list.List
}

type Settings struct {
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

	// Sets the timeout for an idle connection. The default is 30 minutes.
	IdleTimeout time.Duration
}

func DefaultSettings() *Settings {
	return &Settings{
		Timeout:        2 * time.Second,
		CloseOnTimeout: true,
		IdleTimeout:    30 * time.Minute,
	}
}

// EventSource interface provides methods for sending messages and closing all connections.
type EventSource interface {
	// it should implement ServerHTTP method
	http.Handler

	// send message to all consumers
	SendEventMessage(data, event, id string)

	// send retry message to all consumers
	SendRetryMessage(duration time.Duration)

	// consumers count
	ConsumersCount() int

	// close and clear all consumers
	Close()
}

type message interface {
	// The message to be sent to clients
	prepareMessage() []byte
}

func (m *eventMessage) prepareMessage() []byte {
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
			message := em.prepareMessage()
			func() {
				es.consumersLock.RLock()
				defer es.consumersLock.RUnlock()

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
			}()
		case <-es.close:
			close(es.sink)
			close(es.add)
			close(es.staled)
			close(es.close)

			func() {
				es.consumersLock.RLock()
				defer es.consumersLock.RUnlock()

				for e := es.consumers.Front(); e != nil; e = e.Next() {
					c := e.Value.(*consumer)
					close(c.in)
				}
			}()

			es.consumersLock.Lock()
			defer es.consumersLock.Unlock()

			es.consumers.Init()
			return
		case c := <-es.add:
			func() {
				es.consumersLock.Lock()
				defer es.consumersLock.Unlock()

				es.consumers.PushBack(c)
			}()
		case c := <-es.staled:
			toRemoveEls := make([]*list.Element, 0, 1)
			func() {
				es.consumersLock.RLock()
				defer es.consumersLock.RUnlock()

				for e := es.consumers.Front(); e != nil; e = e.Next() {
					if e.Value.(*consumer) == c {
						toRemoveEls = append(toRemoveEls, e)
					}
				}
			}()
			func() {
				es.consumersLock.Lock()
				defer es.consumersLock.Unlock()

				for _, e := range toRemoveEls {
					es.consumers.Remove(e)
				}
			}()
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
	es.sink = make(chan message, 1)
	es.close = make(chan bool)
	es.staled = make(chan *consumer, 1)
	es.add = make(chan *consumer)
	es.consumers = list.New()
	es.timeout = settings.Timeout
	es.idleTimeout = settings.IdleTimeout
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

func (es *eventSource) sendMessage(m message) {
	es.sink <- m
}

func (es *eventSource) SendEventMessage(data, event, id string) {
	em := &eventMessage{id, event, data}
	es.sendMessage(em)
}

func (m *retryMessage) prepareMessage() []byte {
	return []byte(fmt.Sprintf("retry: %d\n\n", m.retry/time.Millisecond))
}

func (es *eventSource) SendRetryMessage(t time.Duration) {
	es.sendMessage(&retryMessage{t})
}

func (es *eventSource) ConsumersCount() int {
	es.consumersLock.RLock()
	defer es.consumersLock.RUnlock()

	return es.consumers.Len()
}
