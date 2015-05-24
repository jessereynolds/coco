package main

import (
	"github.com/BurntSushi/toml"
	"github.com/bulletproofnetworks/marksman/coco/coco"
	collectd "github.com/kimor79/gollectd"
	"gopkg.in/alecthomas/kingpin.v1"
	"log"
)

var (
	configPath = kingpin.Arg("config", "Path to coco config").Default("coco.conf").String()
)

func main() {
	kingpin.Version("1.0.0")
	kingpin.Parse()

	var config coco.Config
	if _, err := toml.DecodeFile(*configPath, &config); err != nil {
		log.Fatalln("fatal:", err)
		return
	}

	// Setup data structures to be shared across components
	//      map[target]map[host]map[metric]lastseen
	mapping := map[string]map[string]map[string]int64{}
	raw := make(chan collectd.Packet, 1000000)
	filtered := make(chan collectd.Packet, 1000000)

	var tiers []coco.Tier
	for k, v := range config.Tiers {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	if len(tiers) == 0 {
		log.Fatal("No tiers configured. Exiting.")
	}

	chans := map[string]chan collectd.Packet{
		"raw":      raw,
		"filtered": filtered,
	}
	go coco.Measure(config.Measure, chans, &tiers, mapping)

	// Launch components to do the work
	go coco.Listen(config.Listen, raw)
	for i := 0; i < 4; i++ {
		go coco.Filter(config.Filter, raw, filtered, mapping)
	}
	go coco.Send(&tiers, filtered, mapping)
	coco.Api(config.Api, &tiers, mapping)
}
