package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

var (
	masterAddr     *net.TCPAddr
	prevMasterAddr *net.TCPAddr
	raddr          *net.TCPAddr
	saddr          *net.TCPAddr

	localAddr    = flag.String("listen", ":9999", "local address")
	sentinelAddr = flag.String("sentinel", ":26379", "remote address")
	masterName   = flag.String("master", "", "name of the master redis node")
)

func main() {
	flag.Parse()

	laddr, err := net.ResolveTCPAddr("tcp", *localAddr)
	if err != nil {
		log.Fatalf("Failed to resolve local address: %s", err)
	}
	saddr, err = net.ResolveTCPAddr("tcp", *sentinelAddr)
	if err != nil {
		log.Fatalf("Failed to resolve sentinel address: %s", err)
	}

	stopChan := make(chan string)
	go master(&stopChan)

	listener, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		log.Fatal(err)
	}

	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			log.Println(err)
			continue
		}
		go proxy(conn, masterAddr, stopChan)
	}
}

func master(stopChan *chan string) {
	var err error
	for {
		// has master changed from last time?
		masterAddr, err = getMasterAddr(saddr, *masterName)
		if err != nil {
			log.Printf("[MASTER] Error polling for new master: %s\n", err)
		}
		if err == nil && masterAddr.String() != prevMasterAddr.String() {
			log.Printf("[MASTER] Master Address changed from %s to %s \n", prevMasterAddr.String(), masterAddr.String())
			prevMasterAddr = masterAddr
			close(*stopChan)
			*stopChan = make(chan string)
		}
		time.Sleep(500 * time.Second)
	}
}

func pipe(r net.Conn, w net.Conn, proxyChan chan<- string) {
	bytes, err := io.Copy(w, r)
	log.Printf("[PROXY %s => %s] Shutting down stream; transferred %s bytes: %s\n", w.RemoteAddr().String(), r.RemoteAddr().String(), bytes, err)
	close(proxyChan)
}

// pass a stopChan to the go routtine
func proxy(client *net.TCPConn, redisAddr *net.TCPAddr, stopChan <-chan string) {
	redis, err := net.DialTimeout("tcp4", redisAddr.String(), 50*time.Millisecond)
	if err != nil {
		log.Printf("[PROXY %s => %s] Can't establish connection: %s\n", client.RemoteAddr().String(), redisAddr.String(), err)
		client.Close()
		return
	}

	log.Printf("[PROXY %s => %s] New connection\n", client.RemoteAddr().String(), redisAddr.String())
	defer client.Close()
	defer redis.Close()

	clientChan := make(chan string)
	redisChan := make(chan string)

	go pipe(client, redis, redisChan)
	go pipe(redis, client, clientChan)

	select {
	case <-stopChan:
	case <-clientChan:
	case <-redisChan:
	}

	log.Printf("[PROXY %s => %s] Closing connection\n", client.RemoteAddr().String(), redisAddr.String())
}

func getMasterAddr(sentinelAddress *net.TCPAddr, masterName string) (*net.TCPAddr, error) {
	conn, err := net.DialTimeout("tcp4", sentinelAddress.String(), 50*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("Can't connect to Sentinel: %s", err)
	}
	defer conn.Close()

	conn.Write([]byte(fmt.Sprintf("sentinel get-master-addr-by-name %s\n", masterName)))

	b := make([]byte, 256)
	_, err = conn.Read(b)
	if err != nil {
		log.Fatal(err)
	}

	parts := strings.Split(string(b), "\r\n")

	if len(parts) < 5 {
		return nil, fmt.Errorf("Unexpected response from Sentinel: %s", string(b))
	}

	//getting the string address for the master node
	stringaddr := fmt.Sprintf("%s:%s", parts[2], parts[4])
	addr, err := net.ResolveTCPAddr("tcp", stringaddr)
	if err != nil {
		return nil, fmt.Errorf("Unable to resolve new master %s: %s", stringaddr, err)
	}

	//check that there's actually someone listening on that address
	conn2, err := net.DialTimeout("tcp", addr.String(), 50*time.Millisecond)
	if err != nil {
		return nil, err
	}
	defer conn2.Close()

	return addr, err
}
