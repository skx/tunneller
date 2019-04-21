//
// Client for our self-hosted ngrok alternative.
//

package main

import (
	"bytes"
	"context"
	b64 "encoding/base64"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"sync"

	"github.com/google/subcommands"
	"github.com/gorilla/websocket"
)

//
// clientCmd is the structure for this sub-command.
//
type clientCmd struct {

	//
	// Mutex protects our state.
	//
	mutex *sync.Mutex

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
}

//
// Glue
//
func (p *clientCmd) Name() string     { return "client" }
func (p *clientCmd) Synopsis() string { return "Launch our client." }
func (p *clientCmd) Usage() string {
	return `client :
  Launch the client, exposing a local service to the internet
`
}

//
// Flag setup
//
func (p *clientCmd) SetFlags(f *flag.FlagSet) {

	f.StringVar(&p.expose, "expose", "", "The host/port to expose to the internet.")
	f.StringVar(&p.tunnel, "tunnel", "tunneller.steve.fi", "The address of the publicly visible tunnel-host")
	f.StringVar(&p.name, "name", "cake", "The name for this connection")
}

//
// Entry-point.
//
func (p *clientCmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {

	p.mutex = &sync.Mutex{}

	//
	// Ensure that we have setup variables
	//
	if p.expose == "" {
		fmt.Printf("You must specify the host:port to expose.\n")
		return 1
	}
	if p.tunnel == "" {
		fmt.Printf("You must specify the URL of the tunnel end-point.\n")
		return 1
	}

	//
	// These are the details of the tunneller-server
	//
	u := url.URL{Scheme: "ws", Host: p.tunnel, Path: "/" + p.name}
	fmt.Printf("Connecting to %s\n", u.String())

	//
	// connect to it
	//
	c, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {

		if err == websocket.ErrBadHandshake {
			fmt.Printf("\tHandshake failed with status %d\n", resp.StatusCode)

			defer resp.Body.Close()
			var body []byte
			body, err = ioutil.ReadAll(resp.Body)
			if err == nil {
				fmt.Printf("\t%s\n\n", body)
			}
		}
		fmt.Printf("Connection failed: %s", err)
		return 1
	}
	defer c.Close()

	//
	// Connected now, show instructions
	//
	fmt.Printf("Visit http://%s.%s to see the local content from %s\n",
		p.name, p.tunnel, p.expose)

	// Loop for messages
	for {
		p.mutex.Lock()
		msgType, message, err := c.ReadMessage()
		p.mutex.Unlock()

		if err != nil {
			fmt.Printf("Error reading the message from the socket: %s", err.Error())
			return 1
		}

		if msgType == websocket.PingMessage {
			fmt.Printf("Got pong-reply\n")
			p.mutex.Lock()
			c.WriteMessage(websocket.PongMessage, nil)
			p.mutex.Unlock()

		}
		if msgType == websocket.TextMessage {

			//
			// At this point we've received a message.
			//
			// Show it
			//
			fmt.Printf("Incoming Request\n----%s\n---\n", message)

			//
			// Make the connection to our proxied host.
			//
			d := net.Dialer{}
			con, err := d.Dial("tcp", p.expose)
			if err != nil {
				//
				// Connection refused talking to the host
				//
				res := `HTTP 200 OK
Connection: close

Remote server was unreachable
`
				safe := b64.StdEncoding.EncodeToString([]byte(res))

				p.mutex.Lock()
				c.WriteMessage(websocket.TextMessage, []byte(safe))
				p.mutex.Unlock()
				continue
			}
			con.Write(message)

			//
			// Read the reply
			//
			var reply bytes.Buffer
			io.Copy(&reply, con)

			//
			// Send it back
			//
			safe := b64.StdEncoding.EncodeToString(reply.Bytes())
			p.mutex.Lock()
			c.WriteMessage(websocket.TextMessage, []byte(safe))
			p.mutex.Unlock()
			fmt.Printf("Sent reply ..\n")

		}
	}
}
