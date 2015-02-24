package main

import (
	"log"
	"strings"
	"bytes"
	"regexp"
	"time"
	"github.com/go-martini/martini"
	"encoding/json"
	"encoding/binary"
	"net/http"
	"net"
	"github.com/BurntSushi/toml"
	"gopkg.in/alecthomas/kingpin.v1"
	consistent "github.com/stathat/consistent"
	collectd "github.com/kimor79/gollectd"
)

// Listen for collectd network packets, parse , and send them over a channel
func Listen(config listenConfig, c chan collectd.Packet, typesdb string) {
	laddr, err := net.ResolveUDPAddr("udp", config.Bind)
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

func Filter(config filterConfig, raw chan collectd.Packet, filtered chan collectd.Packet, servers map[string]map[string]int64) {
	servers["filtered"] = make(map[string]int64)
	for {
		packet := <- raw
		name := metricName(packet)

		re := regexp.MustCompile(config.Blacklist)
		if (re.FindStringIndex(name) == nil) {
			filtered <- packet
		} else {
			servers["filtered"][name] = time.Now().Unix()
			// FIXME(lindsay): log to stdout or a file, based on config setting
		}
	}
}

func Send(config sendConfig, filtered chan collectd.Packet, servers map[string]map[string]int64) {
	targets := config.Targets
	connections := make(map[string]net.Conn, len(targets))
	con := consistent.New()
	for _, t := range(targets) {
		conn, err := net.Dial("udp", t)
		if err != nil {
			log.Printf("error: send: %s: %+v", t, err)
		} else {
			// Only add the target to the hash if the connection can initially be established
			re := regexp.MustCompile("^(127.|localhost)")
			if re.FindStringIndex(conn.RemoteAddr().String()) != nil {
				log.Printf("warning: %s is local. You may be looping metrics back to coco!", conn.RemoteAddr())
				log.Printf("warning: dutifully adding %s anyway, but beware!", conn.RemoteAddr())
			}
			servers[t] = make(map[string]int64)
			connections[t] = conn
			con.Add(t)
		}
	}

	log.Printf("info: send: hash ring has %d members: %s", len(con.Members()), con.Members())
	if len(connections) == 0 {
		log.Fatal("fatal: no valid write targets in consistent hash ring")
	}

	for {
		packet := <- filtered
		// Get the server we should forward the packet to
		server, err := con.Get(packet.Hostname)
		if err != nil {
			log.Fatal(err)
		}
		// Update metadata
		name := metricName(packet)
		servers[server][name] = time.Now().Unix()

		// Dispatch the metric
		payload := Encode(packet)
		connections[server].Write(payload)
	}
}

// Encode a Packet into the collectd wire protocol format.
func Encode(packet collectd.Packet) ([]byte) {
	// String parts have a length of 5, because there is a terminating null byte
	// Numeric parts have a length of 4, because there is no terminating null byte

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

	valuesBuf := make([]byte, 0)
	// Values - Values part
	for _, v := range packet.Values {
		valuesBuf = append(valuesBuf, byte(v.Type))

		if v.Type == collectd.TypeGauge {
			gaugeBytes := new(bytes.Buffer)
			binary.Write(gaugeBytes, binary.LittleEndian, v.Value)
			valuesBuf = append(valuesBuf, gaugeBytes.Bytes()...)
		} else {
			valueBytes := new(bytes.Buffer)
			binary.Write(valueBytes, binary.BigEndian, v.Value)
			valuesBuf = append(valuesBuf, valueBytes.Bytes()...)
		}
	}

	// type(2) + length(2) + number of values(2) == 6
	buf = append(buf, []byte{0, collectd.ParseValues}...) // type
	buf = append(buf, []byte{0, byte(len(valuesBuf) + 6)}...) // length
	buf = append(buf, []byte{0, byte(len(packet.Values))}...) // number of values
	buf = append(buf, valuesBuf...) // values themselves

	return buf
}

type cocoConfig struct {
	Listen	listenConfig
	Filter	filterConfig
	Send	sendConfig
	Api		apiConfig
}

type listenConfig struct {
	Bind	string
	Typesdb	string
}

type filterConfig struct {
	Blacklist	string
}

type sendConfig struct {
	Targets	[]string
}

type apiConfig struct {
	Bind	string
}

var (
	configPath	= kingpin.Arg("config", "Path to coco config").Default("coco.conf").String()
)

func main() {
	kingpin.Version("1.0.0")
	kingpin.Parse()

	var config cocoConfig
	if _, err := toml.DecodeFile(*configPath, &config); err != nil {
		log.Fatalln("fatal:", err)
		return
	}

	servers := map[string]map[string]int64{}
	raw := make(chan collectd.Packet)
	filtered := make(chan collectd.Packet)
	// FIXME(lindsay): do proper argument parsing with kingpin
	go Listen(config.Listen, raw, config.Listen.Typesdb)
	go Filter(config.Filter, raw, filtered, servers)
	go Send(config.Send, filtered, servers)

    m := martini.Classic()
    m.Group("/servers", func(r martini.Router) {
        r.Get("", func() []byte {
            data, _ := json.Marshal(servers)
            return data
        })
    })

	log.Println("Booting web server...")
	log.Fatal(http.ListenAndServe(":9090", m))
}
