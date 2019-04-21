Table of Contents
=================

* [tunneller](#tunneller)
* [Overview](#overview)
* [How it works](#how-it-works)
* [Installation](#installation)
  * [Source Installation go &lt;=  1.11](#source-installation-go---111)
  * [Source installation go  &gt;= 1.12](#source-installation-go---112)
  * [Installation Self-Hosted Server](#installation-self-hosted-server)
* [Outstanding Bits](#outstanding-bits)
* [Github Setup](#github-setup)


# tunneller

Tunneller allows you to expose services which are running on `localhost`, or on your local network, to the entire internet.

This is very useful for testing webhooks, the generation of static-site compilers, and similar things.



## Overview

Assuming you have a service running within your local network, perhaps something as simple as a HTTP server you could access via http://localhost:8080/ you can expose that to the entire internet by running:

    $ tunneller client -expose localhost:8080 -name=example

This will output something like this:

    ..
    Visit http://example.tunneller.steve.fi/ to see your content


The location listed will now be publicly visible to all remote hosts.  As the name implies there is a central-host involved which is in charge of routing/proxying to your local network - in this case that central host is `tunneller.steve.fi`, but the important thing is that you can run your own instance of that server.

This is a self-hosted alternative to a system such as `ngrok`.


## How it works

When a client is launched it creates a web-socket connection to the default remote end-point, `tunneller.steve.fi`, and keeps that connection alive.  A name is also sent for that connection.

Next, when a request comes in for `foo.tunneller.steve.fi` the server can look for an open web-socket connection with the name `foo`, and route the request through it:

* The server sends a "Fetch this URL" request to the client.
* The client makes the request - which will succeed, because that is in the "private" location.
* The response is sent back to the server, from where it is sent to the requesting webserver.


## Installation

There are two ways to install this project from source, which depend on the version of the [go](https://golang.org/) version you're using.

> **NOTE**: If you prefer you can find binary releases upon our [release page](https://github.com/skx/tunneller/releases/)


### Source Installation go <=  1.11

If you're using `go` before 1.11 then the following command should fetch/update `tunneller`, and install it upon your system:

     $ go get -u github.com/skx/tunneller

### Source installation go  >= 1.12

If you're using a more recent version of `go` (which is _highly_ recommended), you need to clone to a directory which is not present upon your `GOPATH`:

    git clone https://github.com/skx/tunneller
    cd deployr
    go install


If you don't have a golang environment setup you should be able to download a binary for GNU/Linux from [our release page](https://github.com/skx/tunneller/releases).




## Installation Self-Hosted Server

If you wish to host your own central-server things are a little more complex:

* You'll need to create a DNS-entry: `tunneller.example.com`
* You'll also need to setup a __wildcard__ DNS entry for `*.tunneller.example.com` to point to the same host.
* Finally you'll need to setup nginx/apache to proxy to the server, which will bind to 127.0.0.1:8080 by default.

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

Note if you're not using the proxy-modules already you'll need:

    a2enmod proxy
    a2enmod proxy_http
    a2enmod proxy_wstunnel



## Outstanding Bits

* SSL won't be supported.


## Github Setup

This repository is configured to run tests upon every commit, and when
pull-requests are created/updated.  The testing is carried out via
[.github/run-tests.sh](.github/run-tests.sh) which is used by the
[github-action-tester](https://github.com/skx/github-action-tester) action.

Releases are automated in a similar fashion via [.github/build](.github/build),
and the [github-action-publish-binaries](https://github.com/skx/github-action-publish-binaries) action.
