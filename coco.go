package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"regexp"
	"time"
	"os"
	"github.com/go-martini/martini"
	"encoding/json"
	"net/http"
	consistent "github.com/stathat/consistent"
	collectd "github.com/kimor79/gollectd"
)

// Listen for collectd network packets, parse , and send them over a channel
func Listen(addr string, c chan collectd.Packet, typesdb string) {
	laddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Fatalln("fatal: failed to resolve address", err)
	}

	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Fatalln("fatal: failed to listen", err)
	}

	types, err := collectd.TypesDBFile(typesdb)
	if err != nil {
		log.Fatalln("fatal: failed to parse types.db", err)
	}

	for {
		// 1452 is collectd 5's default buffer size. See:
		// https://collectd.org/wiki/index.php/Binary_protocol
		buf := make([]byte, 1452)

		n, err := conn.Read(buf[:])
		if err != nil {
			log.Println("error: Failed to receive packet", err)
			continue
		}

		packets, err := collectd.Packets(buf[0:n], types)
		if err != nil {
			log.Println("error: Failed to receive packet", err)
			continue
		}

		for _, p := range *packets {
			c <- p
		}
	}
}

func metricName(packet collectd.Packet) (string) {
	parts := []string{
		packet.Hostname,
		packet.Plugin,
		packet.PluginInstance,
		packet.Type,
		packet.TypeInstance,
	}

	var prts []string

	for _, p := range parts {
		if len(p) != 0 {
			prts = append(prts, p)
		}
	}

	return strings.Join(prts, "/")
}

func Filter(raw chan collectd.Packet, filtered chan collectd.Packet, servers map[string]map[string]int64) {
	servers["filtered"] = make(map[string]int64)
	for {
		packet := <- raw
		name := metricName(packet)

		re := regexp.MustCompile("/(vmem|irq|entropy|users)/")
		if (re.FindStringIndex(name) == nil) {
			filtered <- packet
		} else {
			servers["filtered"][name] = time.Now().Unix()
			// FIXME(lindsay): log to stdout or a file, based on config setting
			fmt.Fprintf(os.Stderr, "Filtering %s\n", name)
		}
	}
}

func Send(targets []string, filtered chan collectd.Packet, servers map[string]map[string]int64) {
	con := consistent.New()
	for _, t := range(targets) {
		con.Add(t)
		servers[t] = make(map[string]int64)
	}

	for {
		packet := <- filtered
		server, err := con.Get(packet.Hostname)
		if err != nil {
			log.Fatal(err)
		}
		name := metricName(packet)
		//fmt.Printf("%s => %s\n", name, server)
		servers[server][name] = time.Now().Unix()
	}
}

func main() {
	servers := map[string]map[string]int64{}
	targets := []string{"alice","bob","charlie","dee"}
	raw := make(chan collectd.Packet)
	filtered := make(chan collectd.Packet)
	//go Listen("127.0.0.1:25826", c, "/usr/share/collectd/types.db")
	// FIXME(lindsay): check this argument exists. check file in argument exists
	// FIXME(lindsay): do proper argument parsing with kingpin
	// FIXME(lindsay): do proper config parsing with toml
	types := os.Args[1]
	go Listen("0.0.0.0:25826", raw, types)
	go Filter(raw, filtered, servers)
	go Send(targets, filtered, servers)

    m := martini.Classic()
    m.Group("/servers", func(r martini.Router) {
        r.Get("", func() []byte {
            data, _ := json.Marshal(servers)
            return data
        })
    })

	log.Fatal(http.ListenAndServe(":9090", m))
}
