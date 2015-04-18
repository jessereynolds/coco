package main

import (
	"log"
	"github.com/BurntSushi/toml"
	"gopkg.in/alecthomas/kingpin.v1"
	"github.com/bulletproofnetworks/marksman/coco/noodle"
	"github.com/bulletproofnetworks/marksman/coco/coco"
)

var (
	configPath	= kingpin.Arg("config", "Path to coco config").Default("coco.conf").String()
)

func main() {
	kingpin.Version("1.0.0")
	kingpin.Parse()

	var config coco.Config
	if _, err := toml.DecodeFile(*configPath, &config); err != nil {
		log.Fatalln("fatal:", err)
		return
	}

	var tiers []coco.Tier
	for k, v := range(config.Send) {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	noodle.Fetch(config.Fetch, &tiers)
}
