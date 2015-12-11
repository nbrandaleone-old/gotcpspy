/*
This program creates a virtual tap in a TCP stream, copying
both input and output into a log file. A second log file
is created with a hex dump of the data.

This program is based upon the following article:
https://pragprog.com/magazines/2012-06/the-beauty-of-concurrency-in-go

	Nick Brandaleone nbrand@mac.com
	November 2015
*/

package main

import (
    "encoding/hex"
 	"flag"
 	"fmt"
 	"net"
 	"os"
    "runtime"
 	"strings"
 	"time"
)

// Global variables
var (
    host *string = flag.String("host", "", "target host or address")
    port *string = flag.String("port", "0", "target port")
    listen_port *string = flag.String("listen_port", "0", "listen port")
)

// Upon error, write error to Stderr
func die(format string, v ...interface{}) {
    os.Stderr.WriteString(fmt.Sprintf(format+"\n", v...))
    os.Exit(1)
}

// Hex dump logger
func connection_logger(data chan []byte, conn_n int, local_info, remote_info string) {
 	log_name := fmt.Sprintf("log-%s-%04d-%s-%s.log", 
                          format_time(time.Now()), conn_n, local_info, remote_info)
  logger_loop(data, log_name)
}

// Binary dump logger
func binary_logger(data chan []byte, conn_n int, peer string) {
 	log_name := fmt.Sprintf("log-binary-%s-%04d-%s.log",
 	                        format_time(time.Now()), conn_n, peer)
 	logger_loop(data, log_name)
}

// Creates a log file, and then blocks for data
func logger_loop(data chan []byte, log_name string) {
    f, err := os.Create(log_name)
 	if err != nil {
 	    die("Unable to create file %s, %v\n", log_name, err)
 	}
 	defer f.Close()     // Ensures that the file will be closed
 	for {
 	    b := <-data       // wait for data on channel 'data'
 	    if len(b) == 0 {  // if empty data is received, we exit
 	        break
 	    }
 	    f.Write(b)
 	    f.Sync()
 	}
}

func format_time(t time.Time) string {
    return t.Format("2006.01.02-15.04.05")  // year.month.day-hour.minute.second
}

func printable_addr(a net.Addr) string {
    return strings.Replace(a.String(), ":", "-", -1)
}
 
type Channel struct {
    from, to              net.Conn
    logger, binary_logger chan []byte
    ack                   chan bool
}

// This is the heart of the program.  It copies both input and output streams
// to a log (two logs - a binary format and a human readible one).
// Any I/O errors are treated like disconnects.
func pass_through(c *Channel) {
    from_peer := printable_addr(c.from.LocalAddr())
 	to_peer := printable_addr(c.to.LocalAddr())
 	
 	b := make([]byte, 10240)
 	offset := 0
 	packet_n := 0
 	for {
 	  n, err := c.from.Read(b)
 	  if err != nil {
 	      c.logger <- []byte(fmt.Sprintf("Disconnected from %s\n", from_peer))
 	      break
 	  }
 	  if n > 0 {
 	      c.logger <- []byte(fmt.Sprintf("Received (#%d, %08X)%d bytes from %s\n",
 	               packet_n, offset, n, from_peer))
 	      c.logger <- []byte(hex.Dump(b[:n]))
 	      c.binary_logger <- b[:n]
 	      c.to.Write(b[:n])
 	      c.logger <- []byte(fmt.Sprintf("Sent (#%d) to %s\n",
 	               packet_n, to_peer))
 	      offset += n
 	      packet_n += 1
 	      }
 	}
 	c.from.Close()
 	c.to.Close()
 	c.ack <- true       // signal to process_connection to shutdown
}

// Processes the entire connection.
//  It connects to the remote socket, measures the duration of the connection,
//  launches the loggers, and finally transfers the two data transferring threads.
func process_connection(local net.Conn, conn_n int, target string) {
    remote, err := net.Dial("tcp", target)
    if err != nil {
	    fmt.Printf("Unable to connect to %s, %v\n", target, err)
	}
	
	local_info := printable_addr(remote.LocalAddr())
    remote_info := printable_addr(remote.RemoteAddr())
	
	started := time.Now()
	
	logger := make(chan []byte)
	from_logger := make(chan []byte)
	to_logger := make(chan []byte)
	ack := make(chan bool)
	
	go connection_logger(logger, conn_n, local_info, remote_info)
	go binary_logger(from_logger, conn_n, local_info)
	go binary_logger(to_logger, conn_n, remote_info)
	
	logger <- []byte(fmt.Sprintf("Connected to %s at %s\n",
	            target, format_time(started)))
	
	go pass_through(&Channel{remote, local, logger, to_logger, ack})
	go pass_through(&Channel{local, remote, logger, from_logger, ack})
	<-ack // Make sure that the both copiers gracefully finish.
	<-ack // a receive statement; result is discarded
	
	finished := time.Now()
	duration := finished.Sub(started)
	logger <- []byte(fmt.Sprintf("Finished at %s, duration %s\n",
	            format_time(started), duration.String()))
	
	logger <- []byte{}      // Stop logger
	from_logger <- []byte{} // Stop "from" binary logger
	to_logger <- []byte{}   // Stop "to" binary logger
}

// Main function
//  Launches the TCP/IP listener
func main() {
    runtime.GOMAXPROCS(runtime.NumCPU())    // use max CPU. Perhaps 2 or 4 is better?
 	flag.Parse()
 	if flag.NFlag() != 3 {
 	    fmt.Printf("usage: gotcpspy -host target_host -port target_port -listen_port local_port\n")
 	    flag.PrintDefaults()
 	    os.Exit(1)
 	}
 	target := net.JoinHostPort(*host, *port)
 	fmt.Printf("Start listening on port %s and forwarding data to %s\n",
 	            *listen_port, target)
 	ln, err := net.Listen("tcp", ":"+*listen_port)
 	if err != nil {
 	    fmt.Printf("Unable to start listener, %v\n", err)
 	    os.Exit(1)
 	}
 	conn_n := 1
 	for {
 	    if conn, err := ln.Accept(); err == nil {
 	        go process_connection(conn, conn_n, target)
 	        conn_n += 1
 	    } else {
 	        fmt.Printf("Accept failed, %v\n", err)
 	    }
 	}
}
