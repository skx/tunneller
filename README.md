# tunneller

Tunneller allows you to expose services which are running on `localhost`, or on your local network, to the entire internet.

This is very useful for testing webhooks, the generation of static-site compilers, and similar things.



## Overview

Assuming you have a service providing a HTTP server accessible to the localhost via a simple URL like this:

* http://localhost:8080/

You can expose that to the entire internet by running:

    $ go run client.go -expose localhost:8080 -name=example

This will output something like this:

    ..
    Visit http://example.tunneller.steve.fi/ to see your content


_That_ location will be accessible to the world, and will route incoming requests to your local server!


## How it works

When a client is launched it creates a web-socket connection to the default remote end-point, `tunneller.steve.fi` in this case, and keeps that connection alive.  A name is also sent for that connection.

Next, when a request comes in for `foo.tunneller.steve.fi` the server can look for an open web-socket connection with the name `foo`, and route the request through it:

* The server sends a "Fetch this URL" request to the client.
* The client makes the request - which will succeed, because that is in the "private" location.
* The response is sent back to the server, from where it is sent to the requesting webserver.


## Installation

The client will connect to `tunneller.example.com`, so you need to point that at the (publicly accessible) host running the server.

You'll also need to setup a wildcard DNS record for `*.tunneller.example.com`, pointing to the same host.

For Apache2 I used this for the main site:

    <VirtualHost 176.9.183.100:80>
      ServerName tunneller.steve.fi
      RewriteEngine On
      RewriteCond %{HTTP:Upgrade} =websocket [NC]
      RewriteRule /(.*)           ws://localhost:8080/$1 [P,L]
      RewriteCond %{HTTP:Upgrade} !=websocket [NC]
      RewriteRule /(.*)           http://localhost:8080/$1 [P,L]
      Use Proxy 8080
    </VirtualHost>

Then this for the wildcard:

    #  HTTP-only.
    <VirtualHost 176.9.183.100:80>
       ServerName a.tunneller.steve.fi
       ServerAlias *.tunneller.steve.fi
       RewriteEngine On
       Use Proxy 8080
    </VirtualHost>

Note:

    a2enmod proxy
    a2enmod proxy_http
    a2enmod proxy_wstunnel



## Cheatsheet

* Start the server
   go run server.go

* Start the client
   go run client.go -expose localhost:1234 -name=unique


## Outstanding Bits

* SSL won't be supported.

* The client chooses the name, we should ensure it is free first.
  * Or let the server chose it.
