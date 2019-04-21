//
// Multiplexing websocket / HTTP-server
//
// Allows proxying between public and private resources via a series of
// long-lived websocket connections.
//

package main

import (
	b64 "encoding/base64"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//
// We want to make sure that we check the origin of any websocket-connections
// and bump the size of the buffers.
//
var upgrader = websocket.Upgrader{
	ReadBufferSize:  2048,
	WriteBufferSize: 2048,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

var mutex = &sync.Mutex{}

// assigned holds a record of IPs that are available/used.
var assigned map[string]*websocket.Conn

type connection struct {
	ws   *websocket.Conn
	send chan []byte
}

//
// This is our entry-point, it presents a simple server on 8080.
//
func main() {

	//
	// Setup a mapping between connections and handlers.
	//
	assigned = make(map[string]*websocket.Conn)

	//
	// We present a HTTP-server
	//
	// But we accept EITHER HTTP or Websockets
	//
	// Then we do the right thing, depending on what we have.
	//
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		//
		// See if we're upgrading to a websocket connection.
		//
		con := r.Header.Get("Connection")
		if strings.Contains(con, "Upgrade") {

			//
			// At this point we've got a known-client.
			//
			// Record their ID in our connection
			//
			// The ID will be client-sent, for now.
			//
			cid := r.URL.Path[1:]

			//
			// Ensure the name isn't already in-use.
			//
			mutex.Lock()
			tmp := assigned[cid]
			mutex.Unlock()

			if tmp != nil {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, "The name you've chosen is already in use.")
				return

			}

			//
			// Upgrade, and handle any upgrade-errors.
			//
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				fmt.Fprintf(w, "Error upgrading the connection to a web-socket %s", err.Error())
				return
			}
			//

			//
			// Store their name / connection in the map.
			//
			mutex.Lock()
			assigned[cid] = conn
			mutex.Unlock()

			// Now we're just going to busy-loop.
			//
			// Ensure that the new connection handles keep-alives properly.
			//
			go func() {
				for {
					mutex.Lock()
					fmt.Printf("Sending ping to client ..\n")
					conn.WriteMessage(websocket.PingMessage, []byte("OK"))
					mutex.Unlock()
					time.Sleep(3 * time.Second)
				}
			}()
		} else {
			//
			// OK we got an incoming HTTP-request.
			//
			// See which vhost the connection was sent to, we assume that the variable
			// part will be the start of the hostname, which will be split by "."
			//
			host := r.Host
			if strings.Contains(host, ".") {
				hsts := strings.Split(host, ".")
				host = hsts[0]
			}

			//
			// Find the client to which to route the request.
			//
			sock := assigned[host]
			if sock == nil {
				fmt.Fprintf(w, "The request cannot be made to '%s' as the host is offline!", host)
				return
			}
			//
			// Dump the request to plain-text
			//
			requestDump, err := httputil.DumpRequest(r, true)
			fmt.Printf("Sending request\n")
			if err == nil {

				//
				// Forward it on.
				//
				mutex.Lock()
				sock.WriteMessage(websocket.TextMessage, []byte(requestDump))
				mutex.Unlock()

			} else {
				fmt.Printf("Error converting the incoming request to plain-text: %s\n", err.Error())
			}
			fmt.Printf("Sent request\n")

			//
			// Wait for the response from the client.
			//
			for {
				fmt.Printf("Reading message from the client ..\n")
				mutex.Lock()
				msgType, msg, err := sock.ReadMessage()
				mutex.Unlock()

				if err != nil {
					fmt.Printf("Error reading from websocket:%s\n", err.Error())
					fmt.Fprintf(w, "Error reading from websocket %s", err.Error())
					return
				}
				if msgType == websocket.TextMessage {

					decoded, err := b64.StdEncoding.DecodeString(string(msg))
					if err != nil {
						fmt.Printf("Error decoded BASE64 from WS:%s\n", err.Error())
						fmt.Fprintf(w, "Error decoded BASE64 from WS:%s\n", err.Error())
						return
					}

					//
					// This is a hack.
					//
					// The response from the client will be:
					//
					//   HTTP 200 OK
					//   Header: blah
					//   Date: blah
					//   [newline]
					//   <html>
					//   ..
					//
					// i.e. It will contain a full-response, headers, and body.
					// So we need to use hijacking to return that to the caller.
					//
					hj, ok := w.(http.Hijacker)
					if !ok {
						http.Error(w, "Webserver doesn't support hijacking", http.StatusInternalServerError)
						return
					}
					conn, bufrw, err := hj.Hijack()
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					// Don't forget to close the connection:
					defer conn.Close()
					fmt.Fprintf(bufrw, "%s", decoded)
					return
				}
			}

			//
			// We shouldn't reach here
			//
			//
			// Send them some text for now.
			//
			fmt.Fprintf(w, "Hi there %s, your request was for %s!", host, r.URL.Path[1:])
			return
		}
	})

	//
	// Bind to :8080.  Assume we'll be proxied.
	//
	http.ListenAndServe("127.0.0.1:8080", nil)
}
