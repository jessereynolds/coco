package coco

import (
	"testing"
	collectd "github.com/kimor79/gollectd"
	"github.com/bulletproofnetworks/marksman/coco/coco"
)

/*
Listen
 - Split a payload into samples
 - increment counter
*/
func TestListenSplitsSamples(t *testing.T) {
	config := coco.ListenConfig{
		Bind: "0.0.0.0:25888",
		Typesdb: "types.db",
	}
	samples := make(chan collectd.Packet)
	go coco.Listen(config, samples)
	t.Fail()
}

/*
Filter
 - Generate metric name
 - Blacklisting
 - Whitelisting
 - increment counter
*/

/*
Send
 - Hash lookup
 - Encode a packet
*/
