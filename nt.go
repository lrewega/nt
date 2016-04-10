package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
)

var (
	useAppend                = flag.Bool("a", false, "Append output to files, rather than overwriting them.")
	keepListening            = flag.Bool("k", false, "Keep listening for connections. Allows for multiple concurrent connections.")
	maxConcurrentConnections = flag.Uint("n", 0, "The maximum number of concurrent connections. Implies -k.")
	useUDP                   = flag.Bool("u", false, "Use UDP instead of TCP.")
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-a] [bind_address:]port:host:hostport [log_file] [response_log_file]\n", os.Args[0])
}

func init() {
	flag.Usage = func() {
		usage()
		flag.PrintDefaults()
	}
}

func NetTee(bindAddress string, hostAddress string, sendWriter io.Writer, recvWriter io.Writer) error {
	protocol := "tcp"

	if *useUDP {
		protocol = "udp"
	}

	var err error

	server, err := net.Listen(protocol, bindAddress)
	if err != nil {
		return err
	}
	defer server.Close()

	pool := make(chan bool, *maxConcurrentConnections)
	for i := uint(0); i < *maxConcurrentConnections; i++ {
		pool <- true
	}

	var wg sync.WaitGroup

	for {
		remote, err := net.Dial(protocol, hostAddress)
		if err != nil {
			break
		}

		conn, err := server.Accept()
		if err != nil {
			remote.Close()
			break
		}

		wg.Add(1)

		go func() {
			remote := remote
			conn := conn
			defer remote.Close()
			defer conn.Close()

			var wgCopy sync.WaitGroup

			out := io.TeeReader(conn, sendWriter)
			wgCopy.Add(1)
			go func() {
				io.Copy(remote, out)
				wgCopy.Done()
			}()
			in := io.TeeReader(remote, recvWriter)
			wgCopy.Add(1)
			go func() {
				io.Copy(conn, in)
				wgCopy.Done()
			}()

			wgCopy.Wait()
			pool <- true
			wg.Done()
		}()

		<-pool

		if *keepListening == false && *maxConcurrentConnections < 1 {
			break
		}
	}

	wg.Wait()

	return err
}

func main() {
	flag.Parse()

	if len(flag.Args()) < 1 || len(flag.Args()) > 3 {
		usage()
		os.Exit(1)
	}

	addressParts := strings.Split(flag.Args()[0], ":")

	switch len(addressParts) {
	case 3:
		// Prefix with an empty
		addressParts = append([]string{""}, addressParts...)
	case 4: // no bind_address
	default:
		fmt.Fprintf(os.Stderr, "Bad connection specification '%s'\n", os.Args[1])
		os.Exit(1)
	}

	bindAddress := strings.Join(addressParts[:2], ":")
	hostAddress := strings.Join(addressParts[2:], ":")

	sendLog := os.Stdout
	recvLog := os.Stdout

	if len(flag.Args()) > 1 {
		logMode := os.O_RDWR | os.O_CREATE
		if *useAppend {
			logMode |= os.O_APPEND
		} else {
			logMode |= os.O_TRUNC
		}

		var err error
		sendLog, err = os.OpenFile(flag.Args()[1], logMode, 0666)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer sendLog.Close()

		recvLog = sendLog

		if len(flag.Args()) > 2 {
			recvLog, err = os.OpenFile(flag.Args()[2], logMode, 0666)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			defer recvLog.Close()
		}
	}

	err := NetTee(bindAddress, hostAddress, sendLog, recvLog)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
