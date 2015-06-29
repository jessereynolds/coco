# Coco

Coco is a Consistent Collectd sharder.

Coco uses a [consistent hash](https://en.wikipedia.org/wiki/Consistent_hashing)
to distribute metrics across multiple storage nodes, allowing you to easily
scale your metrics storage infrastructure horizontally.

There are two parts to Coco:

 - **Coco**, a collectd network server that consistently hashes incoming metrics.
 - **Noodle**, a Visage-compatible HTTP proxy that looks up metrics across
   storage backends.

## Getting started

**FIXME: add instructions on downloading released binaries**

### Coco

1. Edit `coco.conf` to taste (use `coco.sample.conf` as a base).
1. Start the daemon with `coco coco.conf`.
1. Push collectd packets to the bind address in the `[listen]` section in
   `coco.conf`. You can do this with
   [`collectd-tg`](https://collectd.org/documentation/manpages/collectd-tg.1.shtml)
   from the collectd project (e.g. `collectd-tg -d localhost -H 1500 -p 50`), or
   by pointing a local copy of collectd at Coco's bind address.
1. Dump out the entire state of where metrics are being hashed:

   ```
   $ curl http://127.0.0.1:9090/tiers
   [
     {
       "name": "midterm",
       "targets": [
         "10.1.1.111:25826",
         "10.1.1.112:25826",
         "10.1.1.113:25826",
         "10.1.1.114:25826",
       ],
       "shadows": {
         "\u0000": "10.1.1.111:25826",
         "\u0001": "10.1.1.112:25826",
         "\u0002": "10.1.1.113:25826",
         "\u0003": "10.1.1.114:25826",
       },
       "virtual_replicas": 34,
       "routes": {
         "10.1.1.111:25826": {
            ...
         }
       }
     },
     ...
   ]
   ```
1. Perform a lookup to see how the hash function distributes hosts across the circle:

   ```
   $ curl http://127.0.0.1:9090/lookup?name=foo
   {
     "shortterm": "10.1.1.158:25826",
     "midterm": "10.2.2.40:25826"
   }
   ```


### Noodle

1. Edit `coco.conf` to taste (use `coco.sample.conf` as a base).
1. Start the daemon with `noodle coco.conf`.
1. Make requests to Noodle: http://localhost:9080/data/host.example.org/load/load
   This will make Noodle proxy the request to the server that owns the metric,
   per the consistent hash.

## Using

### How do I deploy?

While Coco and Noodle attach to the running console, they should be run as daemons.

How you daemonise processes on your system is up to you and your distro.

You can find Upstart init scripts for Coco and Noodle under `etc/upstart/` in the source.

### What instrumentation is available?

Both Coco and Noodle expose internal counters and gauges about the operations they perform, and how the running instances are configured.

Both tools expose metrics at `/debug/vars`.

Given you're already using collectd to gather metrics, you can use collectd's curl_json plugin to periodically pull these metrics out of Coco and Noodle.

There are sample collectd configuration files under `etc/collectd/` in the source.

#### Coco

Coco exposes many metrics about what it's doing. Here is a description of what those metrics are:

| Name | Description |
| ---- | ----------- |
| `coco.listen.raw` |  Number of collectd packets Coco has pulled off the wire. |
| `coco.listen.decoded` | Number of samples decoded from the collectd packet payload. |
| `coco.filter.accepted` | Number of packets accepted for dispatch to storage target. |
| `coco.filter.rejected` | Number of packets rejected for dispatch to storage target. |
| `coco.send.{{ target }}` | Number of packets dispatched to a storage target. |
| `coco.queues.raw` | Number of samples dispatched from Listen, queued for processing by Filter. |
| `coco.queues.filtered` | Number of samples dispatched from Filter, queued for processing by Send. |
| `coco.lookup.{{ tier }}` | Number of times the tier has been returned in a lookup query at `/lookup`. |
| `coco.hash.hosts.{{ target }}` | Number of hosts hashed to each target. |
| `coco.errors.fetch.receive` | Unsuccessful collectd packet decoding in Listen. |
| `coco.errors.filter.unhandled` | Unhandled panics in Filter. |
| `coco.errors.lookup.hash.get` | Unsuccessful hash lookups for a name. There should be a corresponding log entry for every counter increment. |
| `coco.errors.send.dial` | Unsuccessful connection to target on boot. There should be a corresponding log entry for every counter increment. |
| `coco.errors.send.write` | Unsuccessful dispatch of sample to a target. |

There is also a bunch of keys under `coco.hash.metrics_per_host.{{ tier }}.{{ target }}`. These are summary statistics for the number of metrics per host hashed to each target in each tier. Specifically:

| Name | Description |
| ---- | ----------- |
| `avg` | Average number of metrics per host hashed to target in a tier. |
| `min` | Minimum number of metrics per host hashed to target in a tier. |
| `max` | Maximum number of metrics per host hashed to target in a tier. |
| `95e` | 95th percentile number of metrics per host hashed to target in a tier. |
| `length` | Total number of hosts hashed to a target in a tier. |
| `sum` | Total number of metrics under all hosts hashed to a target in a tier. |

#### Noodle

Noodle exposes many metrics about what it's doing. Here is a description of what those metrics are:

| Name | Description |
| ---- | ----------- |
| `noodle.fetch.bytes.proxied` | Number of bytes proxied from targets to Noodle clients. |
| `noodle.fetch.target.requests.{{ target }}` | Number of requests proxied to a target. |
| `noodle.fetch.target.response.codes.{{ code }}` | Number of responses served to Noodle clients with a specific status code. |
| `noodle.fetch.tier.requests.{{ tier }}` | Number of responses routed and proxied from a tier. |
| `noodle.errors.fetch.con.get` | Unsuccessful hash lookups for a name. There should be a corresponding log entry for every counter increment. |
| `noodle.errors.fetch.http.get` | Unsuccessful HTTP GET requests to a target. |
| `noodle.errors.fetch.ioutil.readall` | Unsuccessful reads of response from a target. |
| `noodle.errors.fetch.json.unmarshal` | Unsuccessful unmarshalings of JSON in response from target. |
| `noodle.errors.fetch.json.marshal` | Unsuccessful marshaling of JSON for response to Noodle client. |

### What performance can I expect?

Bulletproof runs Coco in production on c3.large instances with [GOMAXPROCS](https://golang.org/pkg/runtime/#hdr-Environment_Variables) set to 16, processing ~200,000 distinct metrics submitted at a 10 second interval.

Coco is heavily concurrent. Coco's throughput depends very heavily on the number of cores available to execute Go threads.

The default GOMAXPROCS is 1, which will give you poor performance as soon as you start sending a good volume of metrics to Coco. If you use Coco seriously, you need to tune GOMAXPROC for your environment. As a starting point, the init scripts shipped with Coco set GOMAXPROCS to 16.

### How it will break

#### Performance

If you run Coco in the real world, you may encounter performance problems.

These are some good indicators of problems:

 - The size of `coco.queues.raw` + `coco.queues.filtered`. These show the number of items on the queue (buffered channels) between Listen + Filter + Send. These should be consistently small, all the time. Queue length variability or growth is indicative of poor processing throughput.
 - Changes to `coco.send.{{ target }}`. collectd should dispatch samples to Coco at a constant rate. Coco should also dispatch samples to storage targets at a constant rate. Changes in the send rate should be considered anomalous. The `coco_anomalous_send` check is a good canary for these problems. Drops in send rate are often linked to CPU contention (e.g. another process is using CPU cycles).

#### Functionality

Coco will run as many pre-flight checks as possible on boot to determine if the configuration is not right, and exit immediately. Check stdout for any errors or warnings.

When Coco boots, it attempts to establish a UDP connection to a target. If Coco cannot establish a connection to the target, it will not dispatch any packets to that target. You can see evidence of this behaviour by checking the `coco.errors.send.dial` metric. If this is greater than 0, samples will not be dispatched to a target, and you must restart Coco for it to re-initialise the connection to the target.

### Monitoring

Coco ships some monitoring checks to give you insight into how Coco is running:

 - `anomalous_coco_send` checks if Coco's send behaviour has changed over a time period.
 - `anomalous_coco_errors` checks if Coco's error rate has changed over a time period.

These checks use the Kolmogorov-Smirnov statistical test to check if there is a change in the distribution of the data over time. The KS-test is a computationally cheap method of testing if rates changes over a window of time.

Both of these checks assume you're using collectd to gather Coco's expvar metrics, and serving them up with [Visage](http://visage.io).

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
