package main

import (
	"log"
	"github.com/BurntSushi/toml"
	"gopkg.in/alecthomas/kingpin.v1"
	consistent "github.com/stathat/consistent"
	collectd "github.com/kimor79/gollectd"
	"github.com/bulletproofnetworks/marksman/coco/coco"
)

var (
	configPath	= kingpin.Arg("config", "Path to coco config").Default("coco.conf").String()
)

func main() {
	kingpin.Version("1.0.0")
	kingpin.Parse()

	var config coco.CocoConfig
	if _, err := toml.DecodeFile(*configPath, &config); err != nil {
		log.Fatalln("fatal:", err)
		return
	}

	// Setup data structures to be shared across components
	servers := map[string]map[string]int64{}
	raw := make(chan collectd.Packet)
	filtered := make(chan collectd.Packet)
	var hashes []*consistent.Consistent

	// Launch components to do the work
	go coco.Listen(config.Listen, raw)
	go coco.Filter(config.Filter, raw, filtered, servers)
	go coco.Send(config.Send, filtered, hashes, servers)
	coco.Api(config.Api, hashes, servers)
}