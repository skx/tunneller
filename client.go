//
// Connect to localhost 8080 and proxy content
// to http://localhost:32400/web/index.html
//

package main

import (
	"bytes"
	b64 "encoding/base64"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
)

//
// We proxy here ..
//
var mutex = &sync.Mutex{}

//
// Entry point.
//
func main() {

	//
	// Setup our command-line arguments
	//
	exposeFlag := flag.String("expose", "", "The host/port to expose to the internet.")
	tunnelFlag := flag.String("tunnel", "tunneller.steve.fi", "The address of the publicly visible tunnel-host")
	nameFlag := flag.String("name", "cake", "The name for this connection")
	flag.Parse()

	//
	// Ensure that we have setup variables
	//
	if *exposeFlag == "" {
		fmt.Printf("You must specify the host:port to expose.\n")
		return
	}
	if *tunnelFlag == "" {
		fmt.Printf("You must specify the URL of the tunnel end-point.\n")
		return
	}

	//
	// These are the details of the tunneller-server
	//
	u := url.URL{Scheme: "ws", Host: *tunnelFlag, Path: "/" + *nameFlag}
	fmt.Printf("Connecting to %s\n", u.String())

	//
	// connect to it
	//
	c, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {

		if err == websocket.ErrBadHandshake {
			fmt.Printf("\tHandshake failed with status %d\n", resp.StatusCode)

			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err == nil {
				fmt.Printf("\t%s\n\n", body)
			}
		}
		fmt.Printf("Connection failed: %s", err)
		return
	}
	defer c.Close()

	//
	// Connected now, show instructions
	//
	fmt.Printf("Visit http://%s.%s to see the local content from %s\n",
		*nameFlag, *tunnelFlag, *exposeFlag)

	// Loop for messages
	for {
		mutex.Lock()
		msgType, message, err := c.ReadMessage()
		mutex.Unlock()
		fmt.Printf("%v %v %v\n", msgType, message, err)

		if err != nil {
			fmt.Printf("Error reading the message from the socket: %s", err.Error())
			return
		}

		if msgType == websocket.PingMessage {
			fmt.Printf("Got pong-reply\n")
			mutex.Lock()
			c.WriteMessage(websocket.PongMessage, nil)
			mutex.Unlock()

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
			con, _ := d.Dial("tcp", *exposeFlag)
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
			mutex.Lock()
			c.WriteMessage(websocket.TextMessage, []byte(safe))
			mutex.Unlock()
			fmt.Printf("Sent reply ..\n")

		}
	}
}
