# go-eventsource

_go-eventsource_ provides server-sent events for net/http server.

## Usage

``` go
package main

import (
    eventsource "github.com/antage/go-eventsource/http"
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
```
