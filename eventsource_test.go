package eventsource_test

import (
	eventsource "."
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testEnv struct {
	eventSource eventsource.EventSource
	server      *httptest.Server
}

func setup(t *testing.T) *testEnv {
	t.Log("Setup testing environment")
	e := new(testEnv)
	(*e).eventSource = eventsource.New()
	(*e).server = httptest.NewServer((*e).eventSource)
	return e
}

func teardown(t *testing.T, e *testEnv) {
	t.Log("Teardown testing environment")
	(*e).eventSource.Close()
	(*e).server.Close()
}

func checkError(t *testing.T, e error) {
	if e != nil {
		t.Error(e)
	}
}

func read(t *testing.T, c net.Conn) []byte {
	resp := make([]byte, 1024)
	_, err := c.Read(resp)
	if err != nil && err != io.EOF {
		t.Error(err)
	}
	return resp
}

func startEventStream(t *testing.T, e *testEnv) (net.Conn, []byte) {
	url := (*e).server.URL
	t.Log("open connection")
	conn, err := net.Dial("tcp", strings.Replace(url, "http://", "", 1))
	checkError(t, err)
	t.Log("send GET request to the connection")
	_, err = conn.Write([]byte("GET / HTTP/1.1\n\n"))
	checkError(t, err)

	resp := read(t, conn)
	t.Logf("got response: \n%s", resp)
	return conn, resp
}

func expectResponse(t *testing.T, c net.Conn, expecting string) {
	time.Sleep(100 * time.Millisecond)

	resp := read(t, c)
	if !strings.Contains(string(resp), expecting) {
		t.Errorf("expected:\n%s\ngot:\n%s\n", expecting, resp)
	}
}

func TestConnection(t *testing.T) {
	e := setup(t)
	defer teardown(t, e)

	conn, resp := startEventStream(t, e)
	defer conn.Close()

	if !strings.Contains(string(resp), "HTTP/1.1 200 OK\n") {
		t.Error("the response has no HTTP status")
	}

	if !strings.Contains(string(resp), "Content-Type: text/event-stream\n") {
		t.Error("the response has no Content-Type header with value 'text/event-stream'")
	}
}

func TestMessageSending(t *testing.T) {
	e := setup(t)
	defer teardown(t, e)

	conn, _ := startEventStream(t, e)
	defer conn.Close()

	t.Log("send message 'test'")
	(*e).eventSource.SendMessage("test", "", "")
	expectResponse(t, conn, "data: test\n\n")

	t.Log("send message 'test' with id '1'")
	(*e).eventSource.SendMessage("test", "", "1")
	expectResponse(t, conn, "id: 1\ndata: test\n\n")

	t.Log("send message 'test' with id '1\n1'")
	(*e).eventSource.SendMessage("test", "", "1\n1")
	expectResponse(t, conn, "id: 11\ndata: test\n\n")

	t.Log("send message 'test' with event type 'notification'")
	(*e).eventSource.SendMessage("test", "notification", "")
	expectResponse(t, conn, "event: notification\ndata: test\n\n")

	t.Log("send message 'test' with event type 'notification\n2'")
	(*e).eventSource.SendMessage("test", "notification\n2", "")
	expectResponse(t, conn, "event: notification2\ndata: test\n\n")

	t.Log("send message 'test\ntest2\ntest3\n'")
	(*e).eventSource.SendMessage("test\ntest2\ntest3\n", "", "")
	expectResponse(t, conn, "data: test\ndata: test2\ndata: test3\ndata: \n\n")
}
