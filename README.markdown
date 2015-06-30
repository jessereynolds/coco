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

1. Perform a lookup to see how the hash function distributes hosts across the ring:

   ```
   $ curl http://127.0.0.1:9080/lookup?name=foo
   {
     "shortterm": "10.1.1.158:25826",
     "midterm": "10.2.2.40:25826"
   }
   ```

## Operationalising

### How do I deploy?

While Coco and Noodle attach to the running console, they should be run as daemons.

How you daemonise processes on your system is up to you and your distro.

You can find Upstart init scripts for Coco and Noodle under `etc/upstart/` in the source.

### What instrumentation is available?

Both Coco and Noodle expose internal counters and gauges about the operations they perform, and how the running instances are configured.

Both tools expose metrics at `/debug/vars`.

Given you're already using collectd to gather metrics, you can use collectd's [curl_json plugin](https://collectd.org/wiki/index.php/Plugin:cURL-JSON) to periodically pull these metrics out of Coco and Noodle.

There are sample collectd configuration files under `etc/collectd/` in the source.

#### Coco

Coco exposes many metrics about what it's doing. This is what those metrics are:

| Name | Type | Description |
| ---- | ---- | ----------- |
| `coco.listen.raw` | Counter | Number of collectd packets Coco has pulled off the wire. |
| `coco.listen.decoded` | Counter | Number of samples decoded from the collectd packet payload. |
| `coco.filter.accepted` | Counter | Number of packets accepted for dispatch to storage target. |
| `coco.filter.rejected` | Counter | Number of packets rejected for dispatch to storage target. |
| `coco.send.{{ target }}` | Counter | Number of packets dispatched to a storage target. |
| `coco.queues.raw` | Counter | Number of samples dispatched from Listen, queued for processing by Filter. |
| `coco.queues.filtered` | Counter | Number of samples dispatched from Filter, queued for processing by Send. |
| `coco.lookup.{{ tier }}` | Counter | Number of times the tier has been returned in a lookup query at `/lookup`. |
| `coco.hash.hosts.{{ target }}` | Counter | Number of hosts hashed to each target. |
| `coco.errors.fetch.receive` | Counter | Unsuccessful collectd packet decoding in Listen. |
| `coco.errors.filter.unhandled` | Counter | Unhandled panics in Filter. |
| `coco.errors.lookup.hash.get` | Counter | Unsuccessful hash lookups for a name. There should be a corresponding log entry for every counter increment. |
| `coco.errors.send.dial` | Counter | Unsuccessful connection to target on boot. There should be a corresponding log entry for every counter increment. |
| `coco.errors.send.write` | Counter | Unsuccessful dispatch of sample to a target. |

There is also a bunch of keys under `coco.hash.metrics_per_host.{{ tier }}.{{ target }}`. These are summary statistics for the number of metrics per host hashed to each target in each tier. Specifically:

| Name | Type | Description |
| ---- | ---- | ----------- |
| `avg` | Gauge | Average number of metrics per host hashed to target in a tier. |
| `min` | Gauge | Minimum number of metrics per host hashed to target in a tier. |
| `max` | Gauge | Maximum number of metrics per host hashed to target in a tier. |
| `95e` | Gauge | 95th percentile number of metrics per host hashed to target in a tier. |
| `length` | Gauge | Total number of hosts hashed to a target in a tier. |
| `sum` | Gauge | Total number of metrics under all hosts hashed to a target in a tier. |

#### Noodle

Noodle exposes many metrics about what it's doing. This is what those metrics are:

| Name | Type | Description |
| ---- | ---- | ----------- |
| `noodle.fetch.bytes.proxied` | Counter | Number of bytes proxied from targets to Noodle clients. |
| `noodle.fetch.target.requests.{{ target }}` | Counter | Number of requests proxied to a target. |
| `noodle.fetch.target.response.codes.{{ code }}` | Counter | Number of responses served to Noodle clients with a specific status code. |
| `noodle.fetch.tier.requests.{{ tier }}` | Counter | Number of responses routed and proxied from a tier. |
| `noodle.errors.fetch.con.get` | Counter | Unsuccessful hash lookups for a name. There should be a corresponding log entry for every counter increment. |
| `noodle.errors.fetch.http.get` | Counter | Unsuccessful HTTP GET requests to a target. |
| `noodle.errors.fetch.ioutil.readall` | Counter | Unsuccessful reads of response from a target. |
| `noodle.errors.fetch.json.unmarshal` | Counter | Unsuccessful unmarshalings of JSON in response from target. |
| `noodle.errors.fetch.json.marshal` | Counter | Unsuccessful marshaling of JSON for response to Noodle client. |

### What performance can I expect?

Bulletproof runs Coco in production on c3.large instances with [GOMAXPROCS](https://golang.org/pkg/runtime/#hdr-Environment_Variables) set to 16, processing ~200,000 distinct metrics submitted at a 10 second interval.

Coco is heavily concurrent. Coco's throughput depends very heavily on the number of cores available to execute Go threads.

The default GOMAXPROCS is 1, which will give you poor performance as soon as you start sending a reasonable volume of metrics to Coco. If you use Coco seriously, you need to tune GOMAXPROC for your environment. As a starting point, the init scripts shipped with Coco set GOMAXPROCS to 16.

### How it will break

#### Performance

If you run Coco in the real world, you may encounter performance problems.

These are some good indicators of problems:

 - The size of `coco.queues.raw` + `coco.queues.filtered`. These show the number of items on the queue (buffered channels) between Listen + Filter + Send. These should be consistently small, all the time. Queue length variability or growth is indicative of poor processing throughput.
 - Changes to `coco.send.{{ target }}`. collectd should dispatch samples to Coco at a constant rate. Coco should also dispatch samples to storage targets at a constant rate. Changes in the send rate should be considered anomalous. The `coco_anomalous_send` check is a good canary for these problems. Drops in send rate are often linked to CPU contention (e.g. another process is using CPU cycles).

#### Functionality

Coco will run as many pre-flight checks as possible on boot to determine if the configuration is not right, and exit immediately if so. Check stdout for any errors or warnings.

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
