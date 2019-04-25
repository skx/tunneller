//
// Client for our self-hosted ngrok alternative.
//
// The way that this operates is pretty simple:
//
//  1.  Connect to the named Mosquitto Queue
//
//  2.  Subscribe to /clients/$id
//
//  3.  Wait for an URL to be posted to that topic, when it
//     is we fetch it and return the result.
//
//  4.  Magic.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/subcommands"
	uuid "github.com/satori/go.uuid"
)

//
// clientCmd is the structure for this sub-command.
//
type clientCmd struct {

	//
	// The name we'll access this resource via.
	//
	name string

	//
	// The tunnel end-point
	//
	tunnel string

	//
	// The service to expose.
	//
	expose string

	//
	// A map of the HTTP-status-codes we've returned and their count
	//
	stats map[string]int

	//
	// The recent requests we've seen.
	//
	requests []Request
}

// Name returns the name of this sub-command.
func (p *clientCmd) Name() string { return "client" }

// Synopsis returns the brief description of this sub-command
func (p *clientCmd) Synopsis() string { return "Launch our client." }

// Usage returns details of this sub-command.
func (p *clientCmd) Usage() string {
	return `client :
  Launch the client, exposing a local service to the internet
`
}

// SetFlags configures the flags this sub-command accepts.
func (p *clientCmd) SetFlags(f *flag.FlagSet) {

	f.StringVar(&p.expose, "expose", "", "The host/port to expose to the internet.")
	f.StringVar(&p.tunnel, "tunnel", "tunnel.steve.fi", "The address of the publicly visible tunnel-host")
	f.StringVar(&p.name, "name", "", "The name for this connection")
}

// onMessage is called when a message is received upon the MQ-topic we're
// watching.
//
// We have to peform the HTTP-fetch which is contained within the message,
// and submit the result back to that same topic.
func (p *clientCmd) onMessage(client MQTT.Client, msg MQTT.Message) {

	//
	// Get the text of the request.
	//
	fetch := msg.Payload()

	//
	// If this is one of our replies ignore it.
	//
	if strings.HasPrefix(string(fetch), "X-") {
		return
	}

	//
	// OK if it isn't one of our requests it should be a JSON-object
	//
	var req Request
	err := json.Unmarshal([]byte(fetch), &req)
	if err != nil {
		fmt.Printf("Failed to unmarshal ..: %s\n", err.Error())
		return
	}

	//
	// This is the result we'll publish back onto the topic.
	//
	result := `HTTP/1.0 503 OK
Content-type: text/html; charset=UTF-8
Connection: close

<!DOCTYPE html>
<html>
<body>
<p>The remote server was unreachable.</p>
</body>
</html>`

	//
	// Make the connection to our proxied host.
	//
	d := net.Dialer{}
	con, err := d.Dial("tcp", p.expose)

	//
	// OK we have a default result saved, which shows an error-page.
	//
	// If we didn't actually get an error then save the real response.
	//
	if err == nil {

		//
		// Make the request
		//
		con.Write([]byte(req.Request))

		//
		// Read the reply.
		//
		var reply bytes.Buffer
		io.Copy(&reply, con)

		//
		// Store the result in our string.
		//
		result = string(reply.Bytes())
	}

	//
	// Bump our stats - we keep track of the number of distinct times
	// each HTTP statuscode has been seen.
	//
	// This is grossly inefficient.
	//
	tmp := strings.Split(result, " ")
	if len(tmp) > 1 {
		code := tmp[1]
		p.stats[code]++
	}

	//
	// Save the request away - but only the first line of the request
	//
	tmp2 := strings.Split(req.Request, "\n")
	if len(tmp2) > 1 {
		req.Request = tmp2[0]
	}
	p.requests = append(p.requests, req)

	//
	// Send the reply back to the MQ topic.
	//
	fmt.Printf("Returning response:\n%s\n", result)
	token := client.Publish("clients/"+p.name, 0, false, "X-"+result)
	token.Wait()
}

//
// Execute is the entry-point to this sub-command.
//
// 1. Connect to the tunnel-host.
//
// 2. Subscribe to MQ and await the reception of URLs to fetch.
//
//    (When one is received it will be handled via onMessage.)
//
func (p *clientCmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {

	//
	// Ensure that we have setup variables
	//
	if p.expose == "" {
		fmt.Printf("You must specify the local host:port to expose.\n")
		return 1
	}
	if p.tunnel == "" {
		fmt.Printf("You must specify the tunnel end-point.\n")
		return 1
	}

	//
	// This is optional, but useful.
	//
	if p.name == "" {
		uid := uuid.NewV4()
		p.name = uid.String()
	}

	//
	// Setup a map of our HTTP-status code statistics.
	//
	p.stats = make(map[string]int)

	//
	// Create a channel so that we can be disconnected cleanly.
	//
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	//
	// Setup the server-address.
	//
	opts := MQTT.NewClientOptions().AddBroker(fmt.Sprintf("tcp://%s:1883", p.tunnel))

	//
	// Set our name.
	//
	opts.SetClientID(p.name)

	//
	// Connected now, show instructions
	//
	fmt.Printf("tunneller client launched\n")
	fmt.Printf("=========================\n")
	fmt.Printf("Visit http://%s.%s/ to see the local content from %s\n",
		p.name, p.tunnel, p.expose)

	//
	// Once we're connected we will subscribe to the named topic.
	//
	opts.OnConnect = func(c MQTT.Client) {

		topic := "clients/" + p.name

		if token := c.Subscribe(topic, 0, p.onMessage); token.Wait() && token.Error() != nil {
			fmt.Printf("Failed to subscribe to the MQ-topic:%s\n", token.Error())
			os.Exit(1)
		}
	}

	//
	// Actually establish the MQ connection.
	//
	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Printf("Failed to connect to the MQ-host %s\n", token.Error())
		return 1
	}

	//
	// Wait until we're interrupted.
	//
	<-c

	//
	// Not reached.
	//
	return 0
}
