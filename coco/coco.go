package coco

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"expvar"
	"fmt"
	"github.com/go-martini/martini"
	collectd "github.com/kimor79/gollectd"
	consistent "github.com/stathat/consistent"
	"log"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

func determineProperties(sizes []int) *expvar.Map {
	var sum int
	props := new(expvar.Map).Init()
	for _, n := range sizes {
		sum += n
	}
	// Determine properties
	sort.Ints(sizes)
	if len(sizes) > 0 {
		summary := map[string]int{
			"min":    sizes[0],
			"max":    sizes[len(sizes)-1],
			"length": len(sizes),
			"sum":    sum,
			"95e":    sizes[int(float64(len(sizes))*0.95)],
		}
		mean := float64(sum) / float64(summary["length"])

		// Pack them into an expvar Map
		for k, v := range summary {
			n := new(expvar.Int)
			n.Set(int64(v))
			props.Set(k, n)
		}
		avge := new(expvar.Float)
		avge.Set(mean)
		props.Set("avg", avge)
	}

	return props
}

// calculateTargetSummaryStats builds per-tier, per-target, metric-to-host summary stats
func calculateTargetSummaryStats(tiers *[]Tier) {
	for _, tier := range *tiers {
		totalSizes := []int{}
		tierStats := new(expvar.Map).Init()
		// Determine summary stats per target
		for target, hosts := range tier.Mappings {
			sizes := []int{}
			for _, metrics := range hosts {
				sizes = append(sizes, len(metrics))
				totalSizes = append(totalSizes, len(metrics))
			}
			if len(sizes) == 0 {
				continue
			}
			// Build summary
			props := determineProperties(sizes)
			tierStats.Set(target, props)
		}
		props := determineProperties(totalSizes)
		tierStats.Set("total", props)
		distCounts.Set(tier.Name, tierStats)
	}
}

func Measure(config MeasureConfig, chans map[string]chan collectd.Packet, tiers *[]Tier) {
	tick := time.NewTicker(config.Interval()).C
	for n, _ := range chans {
		log.Println("info: measure: measuring queue", n)
		queueCounts.Set(n, &expvar.Int{})
	}
	for {
		select {
		case <-tick:
			// Queue lengths
			for n, c := range chans {
				queueCounts.Get(n).(*expvar.Int).Set(int64(len(c)))
			}

			// Per-tier, per-target, metric-to-host summary stats
			calculateTargetSummaryStats(tiers)
		}
	}
}

// Listen takes collectd network packets and breaks them into individual samples.
func Listen(config ListenConfig, c chan collectd.Packet) {
	// Initialise the error counts
	errorCounts.Add("fetch.receive", 0)

	laddr, err := net.ResolveUDPAddr("udp", config.Bind)
	if err != nil {
		log.Fatalln("fatal: failed to resolve address", err)
	}

	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Fatalln("fatal: failed to listen", err)
	}

	types, err := collectd.TypesDBFile(config.Typesdb)
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
			errorCounts.Add("fetch.receive", 1)
			continue
		}
		listenCounts.Add("raw", 1)

		packets, err := collectd.Packets(buf[0:n], types)
		for _, p := range *packets {
			listenCounts.Add("decoded", 1)
			c <- p
		}
	}
}

func MetricName(packet collectd.Packet) string {
	prts := []string{
		packet.Plugin,
		packet.PluginInstance,
		packet.Type,
		packet.TypeInstance,
	}

	var parts []string

	for _, p := range prts {
		if len(p) != 0 {
			parts = append(parts, p)
		}
	}

	return strings.Join(parts, "/")
}

func Filter(config FilterConfig, raw chan collectd.Packet, filtered chan collectd.Packet, blacklist chan BlacklistItem) {
	// Initialise the error counts
	errorCounts.Add("filter.unhandled", 0)

	// Track unhandled errors
	defer func() {
		if r := recover(); r != nil {
			errorCounts.Add("filter.unhandled", 1)
		}
	}()

	for {
		packet := <-raw
		name := MetricName(packet)
		full := packet.Hostname + "/" + name

		re := regexp.MustCompile(config.Blacklist)
		if re.FindStringIndex(full) == nil {
			filtered <- packet
			filterCounts.Add("accepted", 1)
		} else {
			blacklist <- BlacklistItem{Packet: packet, Time: time.Now().Unix()}
			filterCounts.Add("rejected", 1)
		}
	}
}

func Blacklist(updates chan BlacklistItem, blacklisted *map[string]map[string]int64) {
	for {
		item := <-updates
		packet := item.Packet
		name := MetricName(item.Packet)
		if (*blacklisted)[packet.Hostname] == nil {
			(*blacklisted)[packet.Hostname] = make(map[string]int64)
		}
		(*blacklisted)[packet.Hostname][name] = item.Time
	}
}

// BuildTiers sets up tiers so it's ready to dispatch metrics
func BuildTiers(tiers *[]Tier) {
	// Initialise the error counts
	errorCounts.Add("send.dial", 0)

	for i, tier := range *tiers {
		// The consistent hashing function used to map sample hosts to targets
		(*tiers)[i].Hash = consistent.New()
		// Shadow names for targets, used to improve hash distribution
		(*tiers)[i].Shadows = make(map[string]string)
		// map that tracks all the UDP connections
		(*tiers)[i].Connections = make(map[string]net.Conn)
		// map that tracks all target -> host -> metric -> last dispatched relationships
		(*tiers)[i].Mappings = make(map[string]map[string]map[string]int64)
		// Set the virtual replica number from magical pre-computed values
		(*tiers)[i].SetMagicVirtualReplicaNumber(len(tier.Targets))

		// Populate ratio counters per tier
		distCounts.Set(tier.Name, new(expvar.Map).Init())

		for it, t := range tier.Targets {
			conn, err := net.Dial("udp", t)
			if err != nil {
				log.Printf("error: send: %s: %+v", t, err)
				errorCounts.Add("send.dial", 1)
			} else {
				// Only add the target to the hash if the connection can initially be established
				re := regexp.MustCompile("^(127.|localhost)")
				if re.FindStringIndex(conn.RemoteAddr().String()) != nil {
					log.Printf("warning: %s is local. You may be looping metrics back to coco!", conn.RemoteAddr())
					log.Printf("warning: send dutifully adding %s to hash anyway, but beware!", conn.RemoteAddr())
				}
				(*tiers)[i].Connections[t] = conn
				(*tiers)[i].Mappings[t] = make(map[string]map[string]int64)
				// Setup a shadow mapping so we get a more even hash distribution
				shadow_t := string(it)
				(*tiers)[i].Shadows[shadow_t] = t
				(*tiers)[i].Hash.Add(shadow_t)
				metricCounts.Set(t, &expvar.Int{})
				hostCounts.Set(t, &expvar.Int{})
			}
		}
	}

	// Log how the hashes are set up
	for _, tier := range *tiers {
		hash := tier.Hash
		var targets []string
		for _, shadow_t := range hash.Members() {
			targets = append(targets, tier.Shadows[shadow_t])
		}
		log.Printf("info: send: tier '%s' hash ring has %d members: %s", tier.Name, len(hash.Members()), targets)
	}

	for _, tier := range *tiers {
		if len(tier.Connections) == 0 {
			log.Fatalf("fatal: send: no targets available in tier %s", tier.Name)
		}
	}
}

func Send(tiers *[]Tier, filtered chan collectd.Packet) {
	// Initialise the error counts
	errorCounts.Add("send.write", 0)

	BuildTiers(tiers)

	for {
		packet := <-filtered
		for _, tier := range *tiers {
			// FIXME(lindsay): fire off a goroutine for dispatch to each tier

			// Get the target we should forward the packet to
			target, err := tier.Lookup(packet.Hostname)
			if err != nil {
				log.Fatal(err)
			}

			// Update metadata
			name := MetricName(packet)
			if tier.Mappings[target][packet.Hostname] == nil {
				tier.Mappings[target][packet.Hostname] = make(map[string]int64)
			}
			tier.Mappings[target][packet.Hostname][name] = time.Now().Unix()

			// Dispatch the metric
			payload := Encode(packet)
			_, err = tier.Connections[target].Write(payload)
			if err != nil {
				errorCounts.Add("send.write", 1)
				continue
			}

			// Update counters
			hostCounts.Get(target).(*expvar.Int).Set(int64(len(tier.Mappings[target])))
			mc := 0
			for _, v := range tier.Mappings[target] {
				mc += len(v)
			}
			metricCounts.Get(target).(*expvar.Int).Set(int64(mc))
			sendCounts.Add(target, 1)
			sendCounts.Add("total", 1)
		}
	}
}

// Encode a Packet into the collectd wire protocol format.
func Encode(packet collectd.Packet) []byte {
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
	if packet.Time > 0 {
		timeBytes := new(bytes.Buffer)
		binary.Write(timeBytes, binary.BigEndian, packet.Time)
		buf = append(buf, []byte{0, collectd.ParseTime}...)
		buf = append(buf, []byte{0, byte(len(timeBytes.Bytes()) + 4)}...)
		buf = append(buf, timeBytes.Bytes()...)
	}

	// TimeHR - Number part
	if packet.TimeHR > 0 {
		timeHRBytes := new(bytes.Buffer)
		binary.Write(timeHRBytes, binary.BigEndian, packet.TimeHR)
		buf = append(buf, []byte{0, collectd.ParseTimeHR}...)
		buf = append(buf, []byte{0, byte(len(timeHRBytes.Bytes()) + 4)}...)
		buf = append(buf, timeHRBytes.Bytes()...)
	}

	// Interval - Number part
	if packet.Interval > 0 {
		intervalBytes := new(bytes.Buffer)
		binary.Write(intervalBytes, binary.BigEndian, packet.Interval)
		buf = append(buf, []byte{0, collectd.ParseInterval}...)
		buf = append(buf, []byte{0, byte(len(intervalBytes.Bytes()) + 4)}...)
		buf = append(buf, intervalBytes.Bytes()...)
	}

	if packet.IntervalHR > 0 {
		intervalHRBytes := new(bytes.Buffer)
		binary.Write(intervalHRBytes, binary.BigEndian, packet.IntervalHR)
		buf = append(buf, []byte{0, collectd.ParseIntervalHR}...)
		buf = append(buf, []byte{0, byte(len(intervalHRBytes.Bytes()) + 4)}...)
		buf = append(buf, intervalHRBytes.Bytes()...)
	}

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
	valuesBuf := make([]byte, 0)

	// Write out the types
	for _, v := range packet.Values {
		valueTypeBytes := new(bytes.Buffer)
		binary.Write(valueTypeBytes, binary.BigEndian, v.Type)
		valuesBuf = append(valuesBuf, valueTypeBytes.Bytes()...)
	}

	// Then write out the values
	for _, v := range packet.Values {
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
	buf = append(buf, []byte{0, collectd.ParseValues}...)     // type
	buf = append(buf, []byte{0, byte(len(valuesBuf) + 6)}...) // length
	buf = append(buf, []byte{0, byte(len(packet.Values))}...) // number of values
	buf = append(buf, valuesBuf...)                           // values themselves

	return buf
}

func TierLookup(params martini.Params, req *http.Request, tiers *[]Tier) []byte {
	// Initialise the error counts
	errorCounts.Add("lookup.hash.get", 0)

	qs := req.URL.Query()
	if len(qs["name"]) > 0 {
		name := qs["name"][0]
		result := map[string]string{}

		for _, tier := range *tiers {
			target, err := tier.Lookup(name)
			if err != nil {
				defer func() {
					errorCounts.Add("lookup.hash.get", 1)
					log.Printf("error: api: %s: %+v\n", name, err)
				}()
			}
			defer func() {
				lookupCounts.Add(tier.Name, 1)
			}()
			result[tier.Name] = target
		}
		json, _ := json.Marshal(result)
		return json
	} else {
		return []byte("")
	}
}

func ExpvarHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(w, "{")
	first := true
	systems := map[string]map[string]interface{}{}
	systems["coco"] = make(map[string]interface{})
	systems["noodle"] = make(map[string]interface{})

	expvar.Do(func(kv expvar.KeyValue) {
		re := regexp.MustCompile("^(coco|noodle)")
		if re.FindStringIndex(kv.Key) != nil {
			parts := strings.SplitN(kv.Key, ".", 2)
			sys := parts[0]
			key := parts[1]
			systems[sys][key] = kv.Value
		} else {
			if !first {
				fmt.Fprintf(w, ",")
			}
			first = false
			fmt.Fprintf(w, "%q: %s", kv.Key, kv.Value)
		}
	})

	for k, _ := range systems {
		first = true
		fmt.Fprintf(w, ",%q: {", k)
		for k, v := range systems[k] {
			if !first {
				fmt.Fprintf(w, ",")
			}
			first = false
			fmt.Fprintf(w, "%q:%s", k, v)
		}
		fmt.Fprintf(w, "}")
	}

	fmt.Fprintf(w, "}\n")
}

func Api(config ApiConfig, tiers *[]Tier, blacklisted *map[string]map[string]int64) {
	m := martini.Classic()
	// Endpoint for looking up what storage nodes own metrics for a host
	m.Get("/lookup", func(params martini.Params, req *http.Request) []byte {
		return TierLookup(params, req, tiers)
	})
	// Dump out the list of targets Coco is hashing metrics to
	m.Group("/tiers", func(r martini.Router) {
		r.Get("", func() []byte {
			data, _ := json.Marshal(*tiers)
			return data
		})
	})
	m.Get("/blacklisted", func(params martini.Params, req *http.Request) []byte {
		data, _ := json.Marshal(*blacklisted)
		return data
	})
	// Implement expvars.expvarHandler in Martini.
	m.Get("/debug/vars", func(w http.ResponseWriter, r *http.Request) {
		ExpvarHandler(w, r)
	})

	log.Printf("info: binding web server to %s", config.Bind)
	log.Fatal(http.ListenAndServe(config.Bind, m))
}

type Config struct {
	Listen  ListenConfig
	Filter  FilterConfig
	Tiers   map[string]TierConfig
	Api     ApiConfig
	Fetch   FetchConfig
	Measure MeasureConfig
}

type ListenConfig struct {
	Bind    string
	Typesdb string
}

type FilterConfig struct {
	Blacklist string
}

type TierConfig struct {
	Targets []string
}

type ApiConfig struct {
	Bind string
}

type FetchConfig struct {
	Bind         string
	ProxyTimeout Duration `toml:"proxy_timeout"`
	// FIXME(lindsay): RemotePort is a bit of a code smell.
	// Ideally every target could define its own port for collectd + Visage.
	RemotePort string `toml:"remote_port"`
}

// Helper function to provide a default timeout value
func (f *FetchConfig) Timeout() time.Duration {
	if f.ProxyTimeout.Duration == 0 {
		return 3 * time.Second
	} else {
		return f.ProxyTimeout.Duration
	}
}

type MeasureConfig struct {
	TickInterval Duration `toml:"interval"`
}

// Helper function to provide a default interval value
func (m *MeasureConfig) Interval() time.Duration {
	if m.TickInterval.Duration == 0 {
		return 10 * time.Second
	} else {
		return m.TickInterval.Duration
	}
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

type Tier struct {
	Name    string                 `json:"name"`
	Targets []string               `json:"targets"`
	Hash    *consistent.Consistent `json:"-"`
	Shadows map[string]string      `json:"shadows"`
	// map[target]map[sample host]map[sample metric name]last dispatched
	Mappings        map[string]map[string]map[string]int64 `json:"routes"`
	Connections     map[string]net.Conn                    `json:"connections,nil"`
	VirtualReplicas int                                    `json:"virtual_replicas"`
}

// Lookup maps a name to a target in a tier's hash
func (t *Tier) Lookup(name string) (string, error) {
	shadow_t, err := t.Hash.Get(name)
	if err != nil {
		return "", err
	}
	target := t.Shadows[shadow_t]
	return target, nil
}

/*
SetMagicVirtualReplicaNumber sets the number of virtual replicas on the hash.

Pass it the number of targets in a tier, and it looks up the optimal number of
virtual replicas in the table of magic numbers and uses that on the hash.

The magic numbers are determined from the results output in consistent_test.go.

There are problems with this approach:

 - You can't change the number of virtual replicas on the hash after you've
   added sites to the hash.
 - If the number of connections actually established is different to the number
   of virtual replicas we set on the hash, we could get poor hashing performance.

For example, if you tell SetMagicVirtualReplicaNumber you have 12 targets and
successfully connect to all of them on boot, your magic number will be 11. But
if you can't connect to even one of them, your magic number will be 100, which
will provide worse hashing performance.
*/
func (t *Tier) SetMagicVirtualReplicaNumber(i int) {
	magics := []int{20, 20, 90, 96, 58, 18, 19, 17, 34, 64, 93, 100, 11, 100, 100, 98, 76, 84, 4, 4, 4, 97, 4, 4, 4, 74, 84, 83, 52, 83, 83, 91, 100, 10, 94, 95, 94, 93, 93, 99, 100, 33, 33, 33, 32, 34, 60, 31, 52, 32, 33, 33, 44, 44, 44, 33, 33, 33, 33, 33, 17, 17, 44, 44, 58, 60, 44, 60, 44, 44, 44, 44, 66, 65, 62, 62, 62, 54, 54, 52, 52, 52, 52, 52, 52, 51, 51, 52, 52, 52, 51, 51, 51, 51, 51, 51, 51, 51, 51, 51, 51}
	number := magics[i]
	t.VirtualReplicas = number
	t.Hash.NumberOfReplicas = number
}

type BlacklistItem struct {
	Packet collectd.Packet
	Time   int64
}

var (
	listenCounts = expvar.NewMap("coco.listen")
	filterCounts = expvar.NewMap("coco.filter")
	sendCounts   = expvar.NewMap("coco.send")
	metricCounts = expvar.NewMap("coco.hash.metrics")
	hostCounts   = expvar.NewMap("coco.hash.hosts")
	distCounts   = expvar.NewMap("coco.hash.metrics_per_host")
	lookupCounts = expvar.NewMap("coco.lookup")
	queueCounts  = expvar.NewMap("coco.queues")
	errorCounts  = expvar.NewMap("coco.errors")
)
