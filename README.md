# eventsource

[![Build Status](https://travis-ci.org/antage/eventsource.svg?branch=master)](https://travis-ci.org/antage/eventsource)

_eventsource_ provides server-sent events for net/http server.

## Usage

### SSE with default options

``` go
package main

import (
    "gopkg.in/antage/eventsource.v1"
    "log"
    "net/http"
    "strconv"
    "time"
)

func main() {
    es := eventsource.New(nil, nil)
    defer es.Close()
    http.Handle("/events", es)
    go func() {
        id := 1
        for {
            es.SendEventMessage("tick", "tick-event", strconv.Itoa(id))
            id++
            time.Sleep(2 * time.Second)
        }
    }()
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### SSE with custom options

``` go
package main

import (
    "gopkg.in/antage/eventsource.v1"
    "log"
    "net/http"
    "strconv"
    "time"
)

func main() {
    es := eventsource.New(
        &eventsource.Settings{
            Timeout: 5 * time.Second,
            CloseOnTimeout: false,
            IdleTimeout: 30 * time.Minute,
        }, nil)
    es.SendRetryMessage(3 * time.Second)
    defer es.Close()
    http.Handle("/events", es)
    go func() {
        id := 1
        for {
            es.SendEventMessage("tick", "tick-event", strconv.Itoa(id))
            id++
            time.Sleep(2 * time.Second)
        }
    }()
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### SSE with custom HTTP headers

``` go
package main

import (
    "gopkg.in/antage/eventsource.v1"
    "log"
    "net/http"
    "strconv"
    "time"
)

func main() {
    es := eventsource.New(
        eventsource.DefaultSettings(),
        func(req *http.Request) [][]byte {
            return [][]byte{
                []byte("X-Accel-Buffering: no"),
                []byte("Access-Control-Allow-Origin: *"),
            }
        },
    )
    defer es.Close()
    http.Handle("/events", es)
    go func() {
        id := 1
        for {
            es.SendEventMessage("tick", "tick-event", strconv.Itoa(id))
            id++
            time.Sleep(2 * time.Second)
        }
    }()
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```
