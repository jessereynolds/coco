package main

import (
	"github.com/BurntSushi/toml"
	"github.com/bulletproofnetworks/marksman/coco/coco"
	"github.com/bulletproofnetworks/marksman/coco/noodle"
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

	var tiers []coco.Tier
	for k, v := range config.Tiers {
		tier := coco.Tier{Name: k, Targets: v.Targets}
		tiers = append(tiers, tier)
	}

	if len(tiers) == 0 {
		log.Fatal("No tiers configured. Exiting.")
	}

	noodle.Fetch(config.Fetch, &tiers)
}
