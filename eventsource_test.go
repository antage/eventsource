package eventsource

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testEnv struct {
	eventSource EventSource
	server      *httptest.Server
}

func setup(t *testing.T) *testEnv {
	t.Log("Setup testing environment")
	e := new(testEnv)
	e.eventSource = New(nil, nil)
	e.server = httptest.NewServer(e.eventSource)
	return e
}

func setupWithHeaders(t *testing.T, headers [][]byte) *testEnv {
	t.Log("Setup testing environment")
	e := new(testEnv)
	e.eventSource = New(
		nil,
		func(*http.Request) [][]byte {
			return headers
		},
	)
	e.server = httptest.NewServer(e.eventSource)
	return e
}

func setupWithCustomSettings(t *testing.T, settings *Settings) *testEnv {
	t.Log("Setup testing environment")
	e := new(testEnv)
	e.eventSource = New(
		settings,
		nil,
	)
	e.server = httptest.NewServer(e.eventSource)
	return e
}

func teardown(t *testing.T, e *testEnv) {
	t.Log("Teardown testing environment")
	e.eventSource.Close()
	e.server.Close()
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
	url := e.server.URL
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

	if !strings.Contains(string(resp), "HTTP/1.1 200 OK\r\n") {
		t.Error("the response has no HTTP status")
	}

	if !strings.Contains(string(resp), "Content-Type: text/event-stream\r\n") {
		t.Error("the response has no Content-Type header with value 'text/event-stream'")
	}
}

func TestConnectionWithCustomHeaders(t *testing.T) {
	e := setupWithHeaders(t, [][]byte{
		[]byte("X-Accel-Buffering: no"),
		[]byte("Access-Control-Allow-Origin: *"),
	})
	defer teardown(t, e)

	conn, resp := startEventStream(t, e)
	defer conn.Close()

	if !strings.Contains(string(resp), "HTTP/1.1 200 OK\r\n") {
		t.Error("the response has no HTTP status")
	}

	if !strings.Contains(string(resp), "Content-Type: text/event-stream\r\n") {
		t.Error("the response has no Content-Type header with value 'text/event-stream'")
	}

	if !strings.Contains(string(resp), "X-Accel-Buffering: no\r\n") {
		t.Error("the response has no X-Accel-Buffering header with value 'no'")
	}

	if !strings.Contains(string(resp), "Access-Control-Allow-Origin: *\r\n") {
		t.Error("the response has no Access-Control-Allow-Origin header with value '*'")
	}
}

func TestRetryMessageSending(t *testing.T) {
	e := setup(t)
	defer teardown(t, e)

	conn, _ := startEventStream(t, e)
	defer conn.Close()

	t.Log("send retry message")
	e.eventSource.SendRetryMessage(3 * time.Second)
	expectResponse(t, conn, "retry: 3000\n\n")
}

func TestEventMessageSending(t *testing.T) {
	e := setup(t)
	defer teardown(t, e)

	conn, _ := startEventStream(t, e)
	defer conn.Close()

	t.Log("send message 'test'")
	e.eventSource.SendEventMessage("test", "", "")
	expectResponse(t, conn, "data: test\n\n")

	t.Log("send message 'test' with id '1'")
	e.eventSource.SendEventMessage("test", "", "1")
	expectResponse(t, conn, "id: 1\ndata: test\n\n")

	t.Log("send message 'test' with id '1\n1'")
	e.eventSource.SendEventMessage("test", "", "1\n1")
	expectResponse(t, conn, "id: 11\ndata: test\n\n")

	t.Log("send message 'test' with event type 'notification'")
	e.eventSource.SendEventMessage("test", "notification", "")
	expectResponse(t, conn, "event: notification\ndata: test\n\n")

	t.Log("send message 'test' with event type 'notification\n2'")
	e.eventSource.SendEventMessage("test", "notification\n2", "")
	expectResponse(t, conn, "event: notification2\ndata: test\n\n")

	t.Log("send message 'test\ntest2\ntest3\n'")
	e.eventSource.SendEventMessage("test\ntest2\ntest3\n", "", "")
	expectResponse(t, conn, "data: test\ndata: test2\ndata: test3\ndata: \n\n")
}

func TestStalledMessages(t *testing.T) {
	e := setup(t)
	defer teardown(t, e)

	conn, _ := startEventStream(t, e)
	conn2, _ := startEventStream(t, e)

	t.Log("send message 'test' to both connections")
	e.eventSource.SendEventMessage("test", "", "")
	expectResponse(t, conn, "data: test\n\n")
	expectResponse(t, conn2, "data: test\n\n")

	conn.Close()
	conn2.Close()

	t.Log("send message with no open connections")
	e.eventSource.SendEventMessage("test", "", "1")

	connNew, _ := startEventStream(t, e)

	t.Log("send a message to new connection")
	e.eventSource.SendEventMessage("test", "", "1\n1")
	expectResponse(t, connNew, "id: 11\ndata: test\n\n")

	for i := 0; i < 10; i++ {
		t.Log("sending additional message ", i)
		e.eventSource.SendEventMessage("test", "", "")
		expectResponse(t, connNew, "data: test\n\n")
	}
}

func TestIdleTimeout(t *testing.T) {
	settings := DefaultSettings()
	settings.IdleTimeout = 500 * time.Millisecond
	e := setupWithCustomSettings(t, settings)
	defer teardown(t, e)

	startEventStream(t, e)

	ccount := e.eventSource.ConsumersCount()
	if ccount != 1 {
		t.Fatalf("Expected 1 customer but got %d", ccount)
	}

	<-time.After(1000 * time.Millisecond)

	ccount = e.eventSource.ConsumersCount()
	if ccount != 0 {
		t.Fatalf("Expected 0 customer but got %d", ccount)
	}
}
