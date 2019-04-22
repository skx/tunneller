package main

import (
	"context"
	b64 "encoding/base64"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"github.com/google/subcommands"
	"github.com/gorilla/websocket"
)

//
// Each incoming websocket-connection will be allocated an instance of this
// because we want to ensure we read/write safely.
//
type connection struct {
	// mutex for safety
	mutex *sync.RWMutex

	// the socket to use to talk to the remote peer.
	socket *websocket.Conn
}

//
// Glue
//
type serveCmd struct {
	// The host we bind upon
	bindHost string

	// the port we bind upon
	bindPort int

	// mutex for safety
	assignedMutex *sync.RWMutex

	// keep track of name/connection pairs
	assigned map[string]*connection
}

func (p *serveCmd) Name() string     { return "serve" }
func (p *serveCmd) Synopsis() string { return "Launch the HTTP server." }
func (p *serveCmd) Usage() string {
	return `serve [options]:
  Launch the HTTP server for proxying via our clients
`
}

//
// Flag setup
//
func (p *serveCmd) SetFlags(f *flag.FlagSet) {
	f.IntVar(&p.bindPort, "port", 8080, "The port to bind upon.")
	f.StringVar(&p.bindHost, "host", "127.0.0.1", "The IP to listen upon.")
}

//
// We want to make sure that we check the origin of any websocket-connections
// and bump the size of the buffers.
//
var upgrader = websocket.Upgrader{
	ReadBufferSize:  2048,
	WriteBufferSize: 2048,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

//
// HTTPHandler is the core of our server.
//
// This function is invoked for all accesses.  However it is complicated
// because it will be invoked in two different roles:
//
//   http://foo.tunneller.example.com/blah
//
//    -> Route the request to the host connected with name "foo".
//
//   ws://tunneller.example.com/foo
//
//    -> Associate the name 'foo' with the long-lived web-socket connection
//
// We can decide at run-time if we're invoked with a HTTP-connection or
// a WS:// connection via the `Connection` header.
//
func (p *serveCmd) HTTPHandler(w http.ResponseWriter, r *http.Request) {

	//
	// See if we're upgrading to a websocket connection.
	//
	con := r.Header.Get("Connection")
	if strings.Contains(con, "Upgrade") {
		p.HTTPHandlerWS(w, r)
	} else {
		p.HTTPHandlerHTTP(w, r)
	}
}

//
// HTTPHandlerHTTP is invoked to forward an incoming HTTP-request
// to the remote host which is tunnelling it.
//
func (p *serveCmd) HTTPHandlerHTTP(w http.ResponseWriter, r *http.Request) {

	//
	// See which vhost the connection was sent to, we assume that
	// the variable part will be the start of the hostname, which will
	// be split by "."
	//
	// i.e. "foo.tunneller.steve.fi" has a name of "foo".
	//
	host := r.Host
	if strings.Contains(host, ".") {
		hsts := strings.Split(host, ".")
		host = hsts[0]
	}

	//
	// Find the client to which to route the request.
	//
	p.assignedMutex.Lock()
	sock := p.assigned[host]
	p.assignedMutex.Unlock()
	if sock == nil {
		fmt.Fprintf(w, "The request cannot be made to '%s' as the host is offline!", host)
		return
	}

	//
	// Dump the request to plain-text
	//
	requestDump, err := httputil.DumpRequest(r, true)
	fmt.Printf("Sending request to remote name %s\n", host)
	if err != nil {
		fmt.Fprintf(w, "Error converting the incoming request to plain-text: %s\n", err.Error())
		fmt.Printf("Error converting the incoming request to plain-text: %s\n", err.Error())
		return
	}

	//
	// Forward it on.
	//
	fmt.Printf("Locking mutex\n")
	sock.mutex.Lock()
	fmt.Printf("Locked mutex\n")
	err = sock.socket.WriteMessage(websocket.TextMessage, []byte(requestDump))
	if err != nil {
		fmt.Printf("Failed to send request down socket %s\n", err.Error())
	}
	sock.mutex.Unlock()
	fmt.Printf("\tRequest sent.\n")

	//
	// Wait for the response from the client.
	//
	response := ""

	for len(response) == 0 {
		fmt.Printf("Awaiting a reply ..\n")

		sock.mutex.Lock()
		msgType, msg, error := sock.socket.ReadMessage()
		sock.mutex.Unlock()
		fmt.Printf("\tReceived something ..\n")

		if error != nil {
			fmt.Printf("\tError reading from websocket:%s\n", error.Error())
			fmt.Fprintf(w, "Error reading from websocket %s", error.Error())
			return
		}
		if msgType == websocket.TextMessage {
			fmt.Printf("\tReply received.\n")

			var raw []byte
			raw, err = b64.StdEncoding.DecodeString(string(msg))
			if err != nil {
				fmt.Printf("Error decoding BASE64 from WS:%s\n", err.Error())
				fmt.Fprintf(w, "Error decoding BASE64 from WS:%s\n", err.Error())
				return
			}

			response = string(raw)
		}
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
		fmt.Printf("Webserver doesn't support hijacking")
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		fmt.Printf("Error running hijack:%s", err.Error())
		return
	}
	// Don't forget to close the connection:
	fmt.Fprintf(bufrw, "%s", response)
	bufrw.Flush()
	conn.Close()

}

//
// HTTPHandlerWS is invoked to handle an incoming websocket request.
//
// If a request is made for http://tunneller.example.com/blah we
// assign the name "blah" to the connection.
//
func (p *serveCmd) HTTPHandlerWS(w http.ResponseWriter, r *http.Request) {

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
	p.assignedMutex.Lock()
	tmp := p.assigned[cid]
	p.assignedMutex.Unlock()

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
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Error upgrading the connection to a web-socket %s", err.Error())
		return
	}

	//
	// Store their name / connection in the map.
	//
	p.assignedMutex.Lock()
	p.assigned[cid] = &connection{mutex: &sync.RWMutex{}, socket: conn}
	p.assignedMutex.Unlock()

	//
	// Now we're just going to busy-loop.
	//
	// Ensuring that we keep the client connection alive.
	//
	go func() {
		//
		// We're connected.
		//
		connected := true

		//
		// Get the structure, we just set.
		//
		p.assignedMutex.Lock()
		connection := p.assigned[cid]
		p.assignedMutex.Unlock()

		//
		// Loop until we get a disconnection.
		//
		for connected {

			//
			// Try to write ..
			//
			connection.mutex.Lock()
			fmt.Printf("Keepalive..\n")
			err := conn.WriteMessage(websocket.PingMessage, []byte("!"))
			connection.mutex.Unlock()

			//
			// If/when it failed ..
			//
			if err != nil {

				//
				// Reap the client.
				//
				fmt.Printf("Client gone away - freeing the name '%s'\n", cid)
				p.assignedMutex.Lock()
				p.assigned[cid] = nil
				p.assignedMutex.Unlock()
				connected = false
				continue
			}

			//
			// Otherwise wait for the future.
			//
			time.Sleep(5 * time.Second)
		}
	}()
}

//
// Entry-point.
//
func (p *serveCmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {

	//
	// Setup a mapping between connections and handlers, and ensure
	// that our mutex is ready.
	//
	p.assigned = make(map[string]*connection)
	p.assignedMutex = &sync.RWMutex{}

	//
	// We present a HTTP-server
	//
	// But we accept EITHER HTTP or Websockets
	//
	// Then we do the right thing, depending on what we have.
	//
	http.HandleFunc("/", p.HTTPHandler)

	//
	// Show where we'll bind
	//
	bind := fmt.Sprintf("%s:%d", p.bindHost, p.bindPort)
	fmt.Printf("Launching the server on http://%s\n", bind)

	//
	// We want to make sure we handle timeouts effectively by using
	// a non-default http-server
	//
	srv := &http.Server{
		Addr:         bind,
		ReadTimeout:  300 * time.Second,
		WriteTimeout: 300 * time.Second,
	}

	//
	// Launch the server.
	//
	err := srv.ListenAndServe()
	if err != nil {
		fmt.Printf("\nError: %s\n", err.Error())
	}

	return 0
}
