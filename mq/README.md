
# Install  mosquitto

For a Debian/Ubuntu system:

    apt-get install mosquitto


# Configure Mosquitto

Create `/etc/mosquitto/conf.d/acl.conf` with just the following contents:

    acl_file /etc/mosquitto/conf.d/acl.txt

Now populate that with:

    topic readwrite clients/#

The result of this will be that __any__ client can connect without any
username/password, and read/write to the topics beneath `clients`.

For example client with the name `cake` can read/write to the topic
`clients/cake`.


## Test Subscription

You should find that you can subscribe to the wildcard topic `clients/#`
via:

     $ mosquitto_sub -v -t clients/#


## Now you're good.

Of course this does mean that clients can sniff on other user's traffic..
