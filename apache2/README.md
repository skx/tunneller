# Apache2 Setup

The following is a complete configuration file for Apache2, as used in
production:

    #
    # This is the bare-tunnel domain.
    #
    # Accesses to this will be websockets, so we need to handle
    # that by using `ws:/`
    #
    <VirtualHost 176.9.183.100:80>
        ServerName tunneller.steve.fi

        RewriteEngine On
        RewriteCond %{HTTP:Upgrade} =websocket [NC]
        RewriteRule /(.*)           ws://localhost:8080/$1 [P,L]
        RewriteCond %{HTTP:Upgrade} !=websocket [NC]
        RewriteRule /(.*)           http://localhost:8080/$1 [P,L]

        <Proxy *>
            Order allow,deny
            Allow from all
        </Proxy>

        ProxyPass        / http://localhost:8080/ Keepalive=On
        ProxyPassReverse / http://localhost:8080/ Keepalive=On

        <Location />
            Order allow,deny
            Allow from all
        </Location>

        ProxyPreserveHost on
        ProxyBadHeader ignore
    </VirtualHost>


    #
    # This is the wildcard virtual-host, which will be accessed by
    # the internet and proxied.
    #
    # We don't need to care about websockets here.
    #
    <VirtualHost 176.9.183.100:80>
         ServerName a.tunneller.steve.fi
        ServerAlias *.tunneller.steve.fi

        <Proxy *>
            Order allow,deny
            Allow from all
        </Proxy>

        ProxyPass        / http://localhost:8080/ Keepalive=On
        ProxyPassReverse / http://localhost:8080/ Keepalive=On

        <Location />
            Order allow,deny
            Allow from all
        </Location>

        ProxyPreserveHost on
        ProxyBadHeader ignore
    </VirtualHost>


## Modules

Note if you're not using the proxy-modules already you'll need:

    a2enmod proxy
    a2enmod proxy_http
    a2enmod proxy_wstunnel
