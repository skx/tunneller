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
// Glue
//
type serveCmd struct {
	// The host we bind upon
	bindHost string

	// the port we bind upon
	bindPort int

	// mutex for safety
	mutex *sync.Mutex

	// keep track of name/connection pairs
	assigned map[string]*websocket.Conn
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

		//
		// OK the request is for a web-socket.
		//
		p.HTTPHandler_WS(w, r)
		return
	}

	//
	// Now we've just got a plain HTTP-request.
	//
	p.HTTPHandler_HTTP(w, r)
}

//
// HTTPHandler_HTTP is invoked to forward an incoming HTTP-request
// to the remote host which is tunnelling it.
//
func (p *serveCmd) HTTPHandler_HTTP(w http.ResponseWriter, r *http.Request) {

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
	sock := p.assigned[host]
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
	p.mutex.Lock()
	sock.WriteMessage(websocket.TextMessage, []byte(requestDump))
	p.mutex.Unlock()
	fmt.Printf("\tRequest sent.\n")

	//
	// Wait for the response from the client.
	//
	for {
		p.mutex.Lock()
		msgType, msg, err := sock.ReadMessage()
		p.mutex.Unlock()

		if err != nil {
			fmt.Printf("Error reading from websocket:%s\n", err.Error())
			fmt.Fprintf(w, "Error reading from websocket %s", err.Error())
			return
		}
		if msgType == websocket.TextMessage {
			fmt.Printf("\tReply received.\n")

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
		}
	}
}

//
// HTTPHandler_WS is invoked to handle an incoming websocket request.
//
// If a request is made for http://tunneller.example.com/blah we
// assign the name "blah" to the connection.
//
func (p *serveCmd) HTTPHandler_WS(w http.ResponseWriter, r *http.Request) {

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
	p.mutex.Lock()
	tmp := p.assigned[cid]
	p.mutex.Unlock()

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
	p.mutex.Lock()
	p.assigned[cid] = conn
	p.mutex.Unlock()

	//
	// Now we're just going to busy-loop.
	//
	// Ensuring that we keep the client connection alive.
	//
	go func() {
		connected := true

		for connected {
			p.mutex.Lock()

			fmt.Printf("Sending ping to client %s\n", cid)
			err := conn.WriteMessage(websocket.PingMessage, []byte("!"))
			if err != nil {
				fmt.Printf("Client gone away - freeing the name '%s'\n", cid)
				p.assigned[cid] = nil
				connected = false
				continue
			}
			p.mutex.Unlock()
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
	p.assigned = make(map[string]*websocket.Conn)
	p.mutex = &sync.Mutex{}

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
