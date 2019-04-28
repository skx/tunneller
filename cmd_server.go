//
// We present ourselves as a HTTP-server.
//
// We assume that *.tunnel.example.com will point to us,
// such that we receive requests for all names.
//
// When a request comes in for the host "foo.tunnel.example.com"
//
//  1. We squirt the incoming request down the MQ topic clients/foo.
//
//  2. We then await a reply, for up to 10 seconds.
//
//       If we receive it great.
//       Otherwise we return an error.
//

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/subcommands"
)

//
// serveCmd is the structure for this sub-command.
//
type serveCmd struct {
	// The host we bind upon
	bindHost string

	// MQ conneciton
	mq MQTT.Client

	// the port we bind upon
	bindPort int
}

// Name returns the name of this sub-command.
func (p *serveCmd) Name() string { return "serve" }

// Synopsis returns the brief description of this sub-command
func (p *serveCmd) Synopsis() string { return "Launch the HTTP server." }

// Usage returns details of this sub-command.
func (p *serveCmd) Usage() string {
	return `serve [options]:
  Launch the HTTP server for proxying via our MQ-connection to the clients.
`
}

// SetFlags configures the flags this sub-command accepts.
func (p *serveCmd) SetFlags(f *flag.FlagSet) {
	f.IntVar(&p.bindPort, "port", 8080, "The port to bind upon.")
	f.StringVar(&p.bindHost, "host", "127.0.0.1", "The IP to listen upon.")
}

//
// RemoteIP retrieves the remote IP address of the requesting HTTP-client.
//
// This is sent to the client, for logging purposes.
//
func RemoteIP(request *http.Request) string {

	//
	// Get the X-Forwarded-For header, if present.
	//
	xForwardedFor := request.Header.Get("X-Forwarded-For")

	//
	// No forwarded IP?  Then use the remote address directly.
	//
	if xForwardedFor == "" {
		ip, _, _ := net.SplitHostPort(request.RemoteAddr)
		return ip
	}

	entries := strings.Split(xForwardedFor, ",")
	address := strings.TrimSpace(entries[0])

	// Remove the port - TODO: IPv6.
	if strings.Contains(address, ":") {
		tmp := strings.Split(address, ":")
		address = tmp[0]
	}

	return (address)
}

//
// HTTPHandler is the core of our server.
//
// This function is invoked for all accesses.
//
func (p *serveCmd) HTTPHandler(w http.ResponseWriter, r *http.Request) {

	//
	// See which vhost the connection was sent to, we assume that
	// the variable part will be the start of the hostname, which will
	// be split by "."
	//
	// i.e. "foo.tunnel.steve.fi" has a name of "foo".
	//
	host := r.Host
	if strings.Contains(host, ".") {
		hsts := strings.Split(host, ".")
		host = hsts[0]
	}

	//
	// Dump the request to plain-text.
	//
	requestDump, err := httputil.DumpRequest(r, true)
	fmt.Printf("Sending request to remote name %s\n", host)
	if err != nil {
		fmt.Fprintf(w, "Error converting the incoming request to plain-text: %s\n", err.Error())
		fmt.Printf("Error converting the incoming request to plain-text: %s\n", err.Error())
		return
	}

	//
	// This is the structure we'll send to the client.
	//
	var req Request

	//
	// Add the actual request.
	//
	req.Request = string(requestDump)

	//
	// Add the source-IP from which it was received.
	//
	req.Source = RemoteIP(r)

	//
	// Convert the structure to a JSON message, so we can send it down
	// the queue.
	//
	toSend, err := json.Marshal(req)

	if err != nil {
		fmt.Fprintf(w, "Error encoding the request as JSON: %s\n", err.Error())
		fmt.Printf("Error encoding the request as JSON: %s\n", err.Error())
		return
	}

	//
	// Publish the JSON object to the topic that we believe the client
	// will be listening upon.
	//
	token := p.mq.Publish("clients/"+host, 0, false, string(toSend))
	token.Wait()

	//
	// The (complete) response from the client will be placed here.
	//
	response := ""

	//
	// Subscribe to the topic.
	//
	subToken := p.mq.Subscribe("clients/"+host, 0, func(client MQTT.Client, msg MQTT.Message) {

		//
		// This function will be executed when a message is received
		//
		// To avoid loops we're making sure that the client publishes
		// its response with a specific-prefix, so that it doesn't
		// treat it as a request to be made.
		//
		// That means that we can identify it here too.
		//
		tmp := string(msg.Payload())
		if strings.HasPrefix(tmp, "X-") {
			response = tmp[2:]
		}
	})
	subToken.Wait()

	//
	// Did we get an error subscribing for the reply?
	//
	if subToken.Error() != nil {
		fmt.Printf("Error subscribing to clients/%s - %s\n", host, subToken.Error())
		fmt.Fprintf(w, "Error subscribing to clients/%s - %s\n", host, subToken.Error())
		return
	}

	//
	// We now busy-wait until we have a reply.
	//
	// We wait for up to ten seconds before deciding the client
	// is either a) offline, or b) failing.
	//
	count := 0
	for len(response) == 0 && count < 40 {

		//
		// Sleep .25 seconds; max count 40, result: 10 seconds.
		//
		fmt.Printf("Awaiting a reply ..\n")
		time.Sleep(250 * time.Millisecond)
		count++
	}

	//
	// Unsubscribe from the topic, regardless of whether we received
	// a response or note.
	//
	// Just to cut down on resource-usage.
	//
	unsubToken := p.mq.Unsubscribe("clients/" + host)
	unsubToken.Wait()
	if unsubToken.Error() != nil {
		fmt.Printf("Failed to unsubscribe from clients/%s - %s\n",
			host, unsubToken.Error())
	}

	//
	// If the length is empty then that means either:
	//
	//   1. We didn't get a reply because the remote host was slow.
	//
	//   2. Nothing is listening on the topic, so the client is dead.
	//
	if len(response) == 0 {

		//
		// Failure-response.
		//
		// NOTE: This is a "complete" response.
		//
		response = `HTTP/1.0 503 OK
Content-type: text/html; charset=UTF-8
Connection: close

<!DOCTYPE html>
<html>
<body>
<p>We didn't receive a reply from the remote host, despite waiting 10 seconds.</p>
</body>
</html>
`
	}

	//
	// The response from the client will be:
	//
	//   HTTP/1.0 200 OK
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

	//
	// Send the reply, and close the connection:
	//
	fmt.Fprintf(bufrw, "%s", response)
	bufrw.Flush()
	conn.Close()

}

// Execute is the entry-point to this sub-command.
func (p *serveCmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {

	//
	// Connect to our MQ instance.
	//
	opts := MQTT.NewClientOptions().AddBroker("tcp://localhost:1883")
	p.mq = MQTT.NewClient(opts)
	if token := p.mq.Connect(); token.Wait() && token.Error() != nil {
		fmt.Printf("Failed to connect to MQ-server: %s\n", token.Error())
		return 1
	}

	//
	// We present a HTTP-server, and we handle all incoming
	// requests (both in terms of path and method).
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
	// NOTE: The timeouts are a little generous, considering our
	// proxy to the client will timeout after 10 seconds..
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
		fmt.Printf("\nError launching our HTTP-server\n:%s\n",
			err.Error())
		return 1
	}

	//
	// Not reached.
	//
	return 0
}
