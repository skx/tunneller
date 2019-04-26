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
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
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
	//   503 -> Service Unavailable
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
		// Only keep the first line.
		req.Request = strings.Replace(tmp2[0], "\r", "", -1)
	}

	//
	// Save the response.
	//
	req.Response = result

	//
	// Record this.
	//
	p.requests = append(p.requests, req)

	//
	// Truncate the list of requests.  We'll keep the most recent
	// five entries here.
	//
	if len(p.requests) > 5 {

		// Work out how many to trim.
		trim := len(p.requests) - 5

		// Do the necessary truncation.
		p.requests = p.requests[trim:]
	}

	//
	// Send the reply back to the MQ topic.
	//
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
	// Setup the server-address.
	//
	opts := MQTT.NewClientOptions().AddBroker(fmt.Sprintf("tcp://%s:1883", p.tunnel))

	//
	// Set our name.
	//
	opts.SetClientID(p.name)

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
	// Setup our GUI
	//
	if err := ui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer ui.Close()

	//
	// Determine our console dimensions.
	//
	termWidth, termHeight := ui.TerminalDimensions()

	//
	// This is the first page.
	//
	// * We show an overview.
	//
	// Page 1 - widget 1 - keyboard
	//
	p11 := widgets.NewParagraph()
	p11.Title = "Keyboard Control"
	p11.Text = "\n  Press q to quit\n  Press h or l to switch tabs, or use the arrow-keys\n\n"
	p11.SetRect(0, 3, termWidth, 9)
	p11.BorderStyle.Fg = ui.ColorYellow

	//
	// Page 1 - widget 2 - access
	//
	p12 := widgets.NewParagraph()
	p12.Title = "Remote Access"
	p12.Text += "\n  http://" + p.name + "." + p.tunnel + "\n\n"
	p12.Text += "  Will proxy content from " + p.expose
	p12.SetRect(0, 10, termWidth, 17)
	p12.BorderStyle.Fg = ui.ColorYellow

	//
	// Page 2 - widget 1 - response-codes
	//
	p21 := widgets.NewBarChart()
	p21.Title = "HTTP Responses"
	p21.SetRect(0, 3, termWidth, termHeight/2)

	//
	// Page 2 - widget 2 - source + request
	//
	p22 := widgets.NewTable()
	p22.Rows = [][]string{
		[]string{"IP Address", "Status", "Request"},
	}
	p22.TextStyle = ui.NewStyle(ui.ColorWhite)
	p22.SetRect(0, (termHeight/2)+1, termWidth, termHeight-3)
	p22.ColumnWidths = []int{20, 8, termWidth - 28}

	updateResponse := func() {
		//
		// We want to show all the distinct status-codes.
		//
		var statsData []float64
		var statsLabel []string

		//
		// We want to sort the keys, so that HTTP-status codes
		// are shown in a logical order.
		//
		var tmp []string
		for k := range p.stats {
			tmp = append(tmp, k)
		}
		sort.Strings(tmp)

		//
		// Update.
		//
		for _, code := range tmp {
			if p.stats[code] > 0 {
				statsLabel = append(statsLabel, code)
				statsData = append(statsData, float64(p.stats[code]))
			}
		}

		//
		// Update the graph and render it.
		//
		p21.Labels = statsLabel
		p21.Data = statsData
		ui.Render(p21)

		//
		// Now update the table.
		//
		var rows [][]string
		rows = append(rows, []string{"IP Address", "Status", "Request"})
		for _, ent := range p.requests {

			//
			// The response is "HTTP XXX BLAH\n.."
			//
			// We only want the status-code.
			//
			// Save the first line in "tmp".
			//
			resLines := strings.Split(ent.Response, "\n")
			tmp := "HTTP -1 OK"
			if len(resLines) > 0 {
				tmp = resLines[0]
			}

			//
			// Get the second token
			//
			resToks := strings.Split(tmp, " ")
			if len(resToks) > 0 {
				tmp = resToks[1]
			}

			//
			rows = append(rows, []string{ent.Source, tmp, ent.Request})
		}
		p22.Rows = rows
		ui.Render(p22)
	}

	//
	// This is our tab-list
	//
	tabpane := widgets.NewTabPane("Overview", "Statistics")
	tabpane.SetRect(0, 0, termWidth, 3)
	tabpane.Border = true

	//
	// The renderTab function will display our tab.
	//
	renderTab := func() {
		switch tabpane.ActiveTabIndex {
		case 0:
			//
			// First tab-pane has a pair of text-widgets.
			//
			ui.Render(p11, p12)
		case 1:
			//
			// Second tab-pane has just a single widget.
			//
			ui.Render(p21, p22)
		}
	}

	//
	// Default to the first tab.
	//
	ui.Render(tabpane, p11, p12)

	//
	// Ensure we can poll for events.
	//
	uiEvents := ui.PollEvents()

	//
	// Also update our graph every half-second.
	//
	ticker := time.NewTicker(500 * time.Millisecond).C

	//
	// Constantly work on our list.
	//
	for {
		select {
		case e := <-uiEvents:
			switch e.ID {

			case "q", "<C-c>":
				return 0
			case "h", "<Left>", "<tab>":
				tabpane.FocusLeft()
				ui.Clear()
				ui.Render(tabpane)
				renderTab()
			case "l", "<Right>":
				tabpane.FocusRight()
				ui.Clear()
				ui.Render(tabpane)
				renderTab()

			case "<Resize>":
				//
				// This just resizes the outline around the tab
				//
				// It doesn't resize the actual widgets upon the
				// tab.
				//
				// Oops!
				//
				payload := e.Payload.(ui.Resize)
				tabpane.SetRect(0, 0, payload.Width, payload.Height)
				ui.Clear()
				ui.Render(tabpane)
				renderTab()
			}
		case <-ticker:

			//
			// If the tab-selected is the stats-page
			// then update our table and graph.
			//
			if tabpane.ActiveTabIndex == 1 {
				updateResponse()
			}
		}
	}

	//
	// Not reached.
	//
	return 0
}
