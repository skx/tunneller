package main

// Request is used for the communication between the client and the
// server.
//
// In our earlier releases we communicated from the server to the client
// via the use of web-sockets.   Now we use a real queue.
//
// The server instructs the client of what it wants to fetch by sending an
// instance of the request-object down the queue.  This structure contains
// the actual request to send:
//
//   GET / HTTP/1.0
//   Host: blah.tunnel.steve.fi
//   ...
//
// As well as that we also send some extra data, currently that is just
// the source IP that made the request for tracking purposes.
//
type Request struct {
	// Request holds the literal HTTP-request which was received
	// by the server and which is to be proxied to the local port.
	Request string

	// Source contains the IP-address of the client which actually
	// made the request.
	Source string

	// Response is the response the client sent.
	// This is only available in the client, but it is exposed here
	// because it does no harm.
	Response string
}
