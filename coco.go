package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"bytes"
	"regexp"
	"time"
	"os"
	"github.com/go-martini/martini"
	"encoding/json"
	"encoding/binary"
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
		fmt.Printf("%+v\n", buf[0:n])
		if err != nil {
			log.Println("error: Failed to receive packet", err)
			continue
		}

		packets, err := collectd.Packets(buf[0:n], types)
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
		servers[server][name] = time.Now().Unix()

		fmt.Printf("%+v\n", Encode(packet))
	}
}

func Encode(packet collectd.Packet) ([]byte) {
	// String parts have a length of 5, because there is a terminating null byte
	// Numeric parts have a length of 4, because there is no terminating null byte
	//fmt.Printf("%+v\n", packet)

	buf := make([]byte, 0)
	null := []byte("\x00")

	// Hostname - String part
	hostBytes := []byte(packet.Hostname)
	buf = append(buf, []byte{0, collectd.ParseHost}...)
	buf = append(buf, []byte{0, byte(len(hostBytes) + 5)}...)
	buf = append(buf, hostBytes...)
	buf = append(buf, null...) // null bytes for string parts

	// Time - Number part
	timeBytes := new(bytes.Buffer)
	binary.Write(timeBytes, binary.BigEndian, packet.Time)
	buf = append(buf, []byte{0, collectd.ParseTime}...)
	buf = append(buf, []byte{0, byte(len(timeBytes.Bytes()) + 4)}...)
	buf = append(buf, timeBytes.Bytes()...)

	// FIXME(lindsay): add encoding of TimeHR

	// Interval - Number part
	intervalBytes := new(bytes.Buffer)
	binary.Write(intervalBytes, binary.BigEndian, packet.Interval)
	buf = append(buf, []byte{0, collectd.ParseInterval}...)
	buf = append(buf, []byte{0, byte(len(intervalBytes.Bytes()) + 4)}...)
	buf = append(buf, intervalBytes.Bytes()...)

	// FIXME(lindsay): add encoding of IntervalHR

	// Plugin - String part
	pluginBytes := []byte(packet.Plugin)
	buf = append(buf, []byte{0, collectd.ParsePlugin}...)
	buf = append(buf, []byte{0, byte(len(pluginBytes) + 5)}...)
	buf = append(buf, pluginBytes...)
	buf = append(buf, null...) // null bytes for string parts

	// PluginInstance - String part
	if len(packet.PluginInstance) > 0 {
		pluginInstanceBytes := []byte(packet.PluginInstance)
		buf = append(buf, []byte{0, collectd.ParsePluginInstance}...)
		buf = append(buf, []byte{0, byte(len(pluginInstanceBytes) + 5)}...)
		buf = append(buf, pluginInstanceBytes...)
		buf = append(buf, null...) // null bytes for string parts
	}

	// Type - String part
	typeBytes := []byte(packet.Type)
	buf = append(buf, []byte{0, collectd.ParseType}...)
	buf = append(buf, []byte{0, byte(len(typeBytes) + 5)}...)
	buf = append(buf, typeBytes...)
	buf = append(buf, null...) // null bytes for string parts

	// TypeInstance - String part
	if len(packet.TypeInstance) > 0 {
		typeInstanceBytes := []byte(packet.TypeInstance)
		buf = append(buf, []byte{0, collectd.ParseTypeInstance}...)
		buf = append(buf, []byte{0, byte(len(typeInstanceBytes) + 5)}...)
		buf = append(buf, typeInstanceBytes...)
		buf = append(buf, null...) // null bytes for string parts
	}

	// Values - Values part
	for _, v := range packet.Values {
		fmt.Printf("%+v\n", v)
	}

	/*
	valuesBytes := new(bytes.Buffer)
	binary.Write(valuesBytes, binary.BigEndian, packet.Values)
	buf = append(buf, []byte{0, collectd.ParseValues}...)
	buf = append(buf, []byte{0, byte(len(valuesBytes.Bytes()) + 4)}...)
	buf = append(buf, timeBytes.Bytes()...)
	*/

	return buf
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

	fmt.Println("running...")
	log.Fatal(http.ListenAndServe(":9090", m))
}
