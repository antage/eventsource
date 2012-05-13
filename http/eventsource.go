package http

import (
	"bytes"
	"container/list"
	"fmt"
	"log"
	"net/http"
	"strings"
)

type eventMessage struct {
	id    string
	event string
	data  string
}

type eventSource struct {
	sink   chan *eventMessage
	staled chan *consumer
	add    chan *consumer
	close  chan bool

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
				go func(c *consumer, message []byte) {
					_, err := c.conn.Write(message)
					if err != nil {
						c.close <- true
						es.staled <- c
					}
				}(e.Value.(*consumer), message)
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
	go controlProcess(es)
	return es
}

func (es *eventSource) Close() {
	es.close <- true
}

// ServeHTTP implements http.Handler interface.
func (es *eventSource) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	cons, err := newConsumer(resp)
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
