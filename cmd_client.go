//
// Client for our self-hosted ngrok alternative.
//
// The way that this operates is pretty simple:
//
//  1.  Generate an ID for ourselves.
//  2.  Connect to the named Mosquitto Queue
//  3.  Subscribe to /clients/$id
//  4.  When a request to fetch an URL is posted to the topic; get it.
//  5.  Post the reply back to the same topic.
//
// There is a simple text-based GUI present, which relies upon keeping
// a few statistics about the requests we've made, and the resulting
// response-code(s).
//

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
	// The tunnel end-point.
	//
	// This is the host to which remote visitors will make their
	// HTTP-requests, and it is also the host which is running an
	// open (!) mosquitto-server.
	//
	tunnel string

	//
	// The service to expose, expressed as 1.2.3.4:NN
	//
	expose string

	//
	// A map of the HTTP-status-codes we've returned and their count.
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
// We have to perform the HTTP-fetch which is contained within the message,
// and submit the result back to that same topic.
func (p *clientCmd) onMessage(client MQTT.Client, msg MQTT.Message) {

	//
	// Get the text of the request.
	//
	fetch := msg.Payload()

	//
	// If this is one of our replies ignore it.
	//
	// Because we receive requests and post the replies upon the
	// same topic we make sure that our replies are prefixed with
	// `X-`, this means we can avoid processing the requests that
	// we sent ourselves.
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

		//
		// TODO: This needs better handling.
		//
		fmt.Printf("Failed to unmarshal ..: %s\n", err.Error())
		return
	}

	//
	// This is the result we'll publish back onto the topic in the case
	// that we cannot successfully communicate with the local service
	// we're trying to expose.
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
	// If we didn't actually get an error then make the actual request,
	// and update with the response we receive.
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
	// Now we have either received a real reply from the service
	// we're exposing, or we've got the fake one we created above.
	//
	// Either way record the request/response, and the HTTP-status
	// code we received.
	//

	//
	// The response will have "HTTP/1.x CODE OK..\n"
	//
	tmp := strings.Split(result, " ")
	if len(tmp) > 1 {
		code := tmp[1]
		p.stats[code]++
	}

	//
	// Save the response.
	//
	req.Response = result

	//
	// Add this request to our list of "recent requests".
	//
	p.requests = append(p.requests, req)

	//
	// And truncate the list, so that we don't consume all our RAM
	// keeping everything.
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
// 2. Subscribe to MQ and await the reception of URLs to fetch.
//    (When one is received it will be handled via onMessage.)
// 3. Present our (read-only) GUI.
//
func (p *clientCmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {

	//
	// Record our launch-time.
	//
	start := time.Now()

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
	// Page 1 - widget 3 - uptime
	//
	p13 := widgets.NewParagraph()
	p13.Title = "Uptime"
	p13.Text += "\n  00:00:00"
	p13.SetRect(0, 18, termWidth, 23)
	p13.BorderStyle.Fg = ui.ColorYellow

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

	//
	// Show our "uptime"
	//
	updateInfo := func() {

		const (
			Decisecond = 100 * time.Millisecond
			Day        = 24 * time.Hour
		)
		ts := time.Since(start)

		sign := time.Duration(1)
		if ts < 0 {
			sign = -1
			ts = -ts
		}
		ts += +Decisecond / 2
		d := sign * (ts / Day)
		ts = ts % Day
		h := ts / time.Hour
		ts = ts % time.Hour
		m := ts / time.Minute
		ts = ts % time.Minute
		s := ts / time.Second
		ts = ts % time.Second

		if d > 0 {
			if d == 1 {
				p13.Text = fmt.Sprintf("%02d day %02d:%02d:%02d", d, h, m, s)
			} else {

				p13.Text = fmt.Sprintf("%02d days %02d:%02d:%02d", d, h, m, s)
			}
		} else {
			p13.Text = fmt.Sprintf("%02d:%02d:%02d", h, m, s)
		}

		p13.Text = "\n  " + p13.Text
		ui.Render(p13)
	}

	//
	// Update the graph / table in the second page.
	//
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
			// The request will be a multi-line thing.
			//
			request := ent.Request
			reqRows := strings.Split(request, "\n")
			if len(reqRows) > 0 {
				request = reqRows[0]
			}

			rows = append(rows, []string{ent.Source, tmp, request})
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
			// First tab-pane.
			//
			ui.Render(p11, p12, p13)
		case 1:
			//
			// Second tab-pane.
			//
			ui.Render(p21, p22)
		}
	}

	//
	// Default to the first tab.
	//
	ui.Render(tabpane, p11, p12, p13)

	//
	// Ensure we can poll for events.
	//
	uiEvents := ui.PollEvents()

	//
	// Also update our dynamic entries every half-second.
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
			// If the tab-selected is our index-page
			// then update our run-time
			//
			if tabpane.ActiveTabIndex == 0 {
				updateInfo()
			}

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
