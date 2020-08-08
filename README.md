# tiproxy

Proxy accepts connections and translates them into
the child process. It maintains a single child process
when there are active incoming connections.

### Quick start

#### curl+ncat

    $ go build 
    $ ./tiproxy --dest 127.0.0.1:3000 --exec ncat --workdir /usr/sbin -- -l -k -p 3000 -4 -v
    $ curl -v http://127.0.0.1:4000/api
    
#### broker-ncat+nc

    $ go build 
    $ ./tiproxy --dest 127.0.0.1:3000 --exec ncat --workdir /usr/sbin -- -l -k -p 3000 -4 -v --broker
    terminal X: $ nc -t 127.0.0.1 4000
    terminal Y: $ echo HELLO | nc -t 127.0.0.1 4000
    terminal Z: $ telnet 127.0.0.1 4000
    terminal Z: > blah [ press ENTER ]

### TiDB with docker-compose

    $ go build
    $ cp tiproxy docker/bin
    $ docker build -t tiproxy:latest ./docker
    $ docker-compose -f ./docker/docker-compose.yml up 
    $ mysql --host 127.0.0.1 --port 4000 -u root
    $ mysql --host 127.0.0.1 --port 4000 -u root
