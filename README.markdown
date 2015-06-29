# Coco

Coco is a Consistent Collectd sharder.

Coco uses a [consistent hash](https://en.wikipedia.org/wiki/Consistent_hashing)
to distribute metrics across multiple storage nodes, allowing you to easily
scale your metrics storage infrastructure horizontally.

There are two parts to Coco:

 - **Coco**, a collectd network server that consistently hashes incoming metrics.
 - **Noodle**, a Visage-compatible HTTP proxy that looks up metrics across
   storage backends.

## Using

**FIXME: add instructions on downloading released binaries**

### Coco

1. Edit `coco.conf` to taste (use `coco.sample.conf` as a base).
1. Start the daemon with `coco coco.conf`.
1. Push collectd packets to the bind address in the `[listen]` section in
   `coco.conf`. You can do this with
   [`collectd-tg`](https://collectd.org/documentation/manpages/collectd-tg.1.shtml)
   from the collectd project (e.g. `collectd-tg -d localhost -H 1500 -p 50`), or
   by pointing a local copy of collectd at Coco's bind address.
1. Inspect what Coco is doing by visiting it's metrics, e.g.
   http://localhost:9090/debug/vars
1. Dump out the entire state of where metrics are being hashed:
   http://localhost:9090/targets

### Noodle

1. Edit `coco.conf` to taste (use `coco.sample.conf` as a base).
1. Start the daemon with `noodle coco.conf`.
1. Make requests to Noodle: http://localhost:9080/data/host.example.org/load/load
   This will make Noodle proxy the request to the server that owns the metric,
   per the consistent hash.
1. Inspect what Noodle is doing by visiting it's metrics, e.g.
   http://localhost:9090/debug/vars

## Developing

Run up a development copy of Coco with:

```
git clone git@github.com:bulletproofnetworks/coco.git
cd coco
# Ensure Marksman is added to your GOPATH, so the servers build.
mkdir -p $GOPATH/src/github.com
ln -sf $(pwd)/.. $GOPATH/src/github.com/bulletproofnetworks
# Setup development helpers.
bundle
cp coco.sample.conf coco.test.conf
# edit targets in coco.test.conf
foreman start
```

The `foreman start` brings up a copy of both Coco and Noodle.

We wrap the `go run` command with foreman, because we unfortunately have to muck
with your `GOPATH` to vendor depedencies. We vendor everything so you don't have
to worry about running `go get` to get every dependency. It's the
[recommended pattern](http://peter.bourgon.org/go-in-production/#dependency-management),
and we frequently have problems where [gopkg.in](http://gopkg.in) goes down.

To push test data into Coco, run:

```
collectd-tg -d localhost -H 1500 -p 50
```

Both Coco and Noodle expose [expvar](http://golang.org/pkg/expvar/) counters
that track the internal behaviour of each server:

 - **Coco**: http://localhost:9090/debug/vars
 - **Noodle**: http://localhost:9080/debug/vars
