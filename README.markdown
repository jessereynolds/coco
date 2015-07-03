# Coco

Coco is a Consistent Collectd sharder.

Coco uses a [consistent hash](https://en.wikipedia.org/wiki/Consistent_hashing)
to distribute metrics across multiple storage nodes, allowing you to easily
scale your metrics storage infrastructure horizontally.

There are two parts to Coco:

 - **Coco**, a collectd network server that consistently hashes incoming metrics to a ring of storage targets.
 - **Noodle**, a Visage-compatible HTTP proxy that looks up metrics across storage targets.

## Why Coco?

**FIXME: explain the rationale**

## Quick start

Download and unpack the [latest release from GitHub](https://github.com/bulletproofnetworks/coco/releases). For example:

```
wget https://github.com/bulletproofnetworks/coco/releases/download/v0.9.0/coco-0.9.0.tar.gz
tar zxvf coco-0.9.0.tar.gz
cd coco
```

### Coco

1. Edit `coco.conf` to taste (use `coco.sample.conf` as a base).
1. Start the daemon with `coco coco.conf`.
1. Push collectd packets to the bind address in the `[listen]` section in `coco.conf`. You can do this with [`collectd-tg`](https://collectd.org/documentation/manpages/collectd-tg.1.shtml):

   ```
   collectd-tg -d localhost -H 1500 -p 50
   ```

   Alternatively you can point a local copy of collectd at Coco:

   ```
   LoadPlugin network
   <Plugin "network">
     Server "127.0.0.1"
   </Plugin>
   ```

### Noodle

1. Edit `coco.conf` to taste (use `coco.sample.conf` as a base).
1. Start the daemon with `noodle coco.conf`.
1. Make a request to Noodle:

   ```
   $ curl http://localhost:9080/data/host.example.org/load/load
   {
     "_meta": {
       "host": "10.1.1.113",
       "target": "10.1.1.113:25826",
       "url": "http://10.1.1.113/data/host.example.org/load/load"
     },
     "host.example.org": {
       "load": {
         "load": {
           "longterm": {
             "data": [
               0.18349999999999997,
               ...
             }
           }
         }
       }
     }
   }
   ```

   This will make Noodle proxy the request to the target that owns the metric,
   per the consistent hash.

## Using

Both Coco and Noodle are configured with a [TOML](https://github.com/toml-lang/toml) formatted config file, passed as the first argument:

```
coco coco.conf &
noodle noodle.conf
```

Conceptually, Coco is a pipeline of components that work together to distribute metrics. Metrics flow from Listen, to Filter, to Send:

 - Listen takes collectd network packets and breaks them into individual samples.
 - Filter drops samples that match a blacklist regex.
 - Send distributes the remaining samples to the storage targets.

Coco also has API and Measure components:

 - API exposes Coco's internal state, and provides metrics about how Coco is performing.
 - Measure periodically samples queue lengths and calculates summary statistics for host-to-metric distributions

Noodle is a single component, Fetch, which proxies requests for metrics to storage targets.

### Tiers

Tiers are a core concept in Coco and Noodle.

A tier is a group of storage targets that metrics can be dispatched to. Tiers let you distribute the same metrics to multiple  storage infrastructures in parallel.

This gives you the flexibility to compose a metric storage platform with multiple storage tiers with different retention policies, storage technologies, and performance characteristics.

When Coco dispatches a sample, it will iterate through all tiers, and for each:

 - Hash the sample to a target in that tier.
 - Dispatch that sample to the hashed target in the tier.

Currently Noodle will only fetch metrics from the first configure tier. Future work on Noodle will be focused on supporting fetching from multiple tiers with different fetch strategies. We will make fetch happen.

### Configuring

Coco and Noodle's configuration file contains sections for controlling their respective components. Not all components have configuration.

#### Tiers

Tier configuration is shared between Coco and Noodle.

A tier must have a name, as specified after the `.` in the tier section name:

```
[tiers.short]
```

Under each tier, there is a single option:

 - `targets`: an array of addresses of storage targets

At least one tier must be configured. Coco and Noodle will error out on boot if no tiers are configured.

**You must ensure that Coco and Noodle have exactly the same tier configuration.**

If the tier configuration differs between Coco and Noodle, you will dispatch metrics to different hosts than you fetch them from.

Example configuration:

```
[tiers]

[tiers.short]
targets = [ "alice:25826", "bob:25826" ]

[tiers.mid]
targets = [ "carol:25826", "dan:25826" ]
```

This configuration is the perfect candidate for generation from a configuration management tool, or derived from Consul or etcd with confd.

#### Listen

Used by Coco.

Options:

 - `bind`: address to listen for incoming collectd packets.
 - `typesdb`: path to collectd's types.db, used to decode the collectd packet payload into the correct value types.

Example configuration:

```
[listen]
bind = "0.0.0.0:25826"
typesdb = "/usr/share/collectd/types.db"
```

#### Filter

Used by Coco.

Options:

 - `blacklist`: a regex applied to all samples to determine if they should be dropped before dispatch to a storage target.

Example configuration:

```
[filter]
blacklist = "/(vmem|irq|entropy|users)/"
```

#### API

Used by Coco.

Options:

 - `bind`: address to serve HTTP requests.

Example configuration:

```
[api]
bind = "0.0.0.0:9090"
```

#### Measure

Used by Coco.

Options:

 - `interval`: how often to generate host-to-metric summary statistics and measure queue lengths.

Example configuration:

```
[measure]
interval = "5s"
```

#### Fetch

Used by Noodle.

Options:

 - `bind`: address to serve HTTP requests.
 - `proxy_timeout`: timeout for HTTP requests to storage targets.
 - `remote_port`: port to connect to all targets when proxying.

Example configuration:

```
[fetch]
bind = "0.0.0.0:9080"
proxy_timeout = "10s"
```

### Querying

You can poke at Coco and Noodle to get information on how they see the world.

For Coco:

 - `/lookup` shows which storage targets in each tier are responsible for a given host's metrics, as specified by the `?name` parameter:

   ```
   $ curl http://127.0.0.1:9080/lookup?name=foo
   {
     "shortterm": "10.1.1.158:25826",
     "midterm": "10.2.2.40:25826"
   }
   ```

 - `/tiers` dumps out the running state for all tiers:

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

 - `/blacklisted` returns all metrics that have been dropped by the Filter, and when they were last seen:

   ```
   $ curl http://127.0.0.1:9090/blacklisted
   {
     "alice.example.org": {
       "entropy/entropy": 1435639791,
       "irq/irq/0": 1435639791,
       "irq/irq/1": 1435639791,
       "irq/irq/12": 1435639791,
       "irq/irq/14": 1435639791,
       "irq/irq/15": 1435639791,
       "irq/irq/24": 1435639791,
       ...
     }
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
| `coco.errors.buildtiers.dial` | Counter | Unsuccessful connection to target on boot. There should be a corresponding log entry for every counter increment. |
| `coco.errors.send.write` | Counter | Unsuccessful dispatch of sample to a target. |
| `coco.errors.send.disconnected` | Counter | Skipped dispatch of sample to a target because no connection was available. |

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
 - `anomalous_coco_errors` checks if Coco's send error rate has changed over a time period.

These checks use the [Kolmogorov-Smirnov](http://www.physics.csbsju.edu/stats/KS-test.html) statistical test to check if there is a change in the distribution of the data over time. The KS-test is a computationally cheap method of testing if rates changes over a window of time.

Both of these checks assume you're using collectd to gather Coco's expvar metrics, and serving them up with [Visage](http://visage.io).

## Help

[Create a GitHub Issue](https://github.com/bulletproofnetworks/coco/issues/new?labels=Question) and we'll do our best to answer your question.

## Developing

The ensure a consistent experience, the development and testing process is wrapped by Docker.

Run up a development copy of Coco with:

``` bash
git clone git@github.com:bulletproofnetworks/coco.git
cd coco
cp coco.sample.conf coco.conf # edit tiers in coco.conf if needed
make run
```

We vendor everything so you don't have to worry about pulling dependencies off the internet. It's a [recommended pattern](http://peter.bourgon.org/go-in-production/#dependency-management), and we frequently have problems where [gopkg.in](http://gopkg.in) goes down.

To push test data into Coco, run:

``` bash
collectd-tg -d localhost -H 1500 -p 50
```

Both Coco and Noodle expose [expvar](http://golang.org/pkg/expvar/) counters that track the internal behaviour of each server:

 - **Coco**: http://localhost:9090/debug/vars
 - **Noodle**: http://localhost:9080/debug/vars

To run the tests:

``` bash
make test
```

## Releasing

To build a release for publishing to GitHub, run:

``` bash
make release
```

This produces a tarball at `./coco.tar.gz`, which you can upload to the GitHub repo as a release artifact.

## Contributing

All contributions are welcome: ideas, patches, documentation, bug reports, questions, and complaints.

Coco is [MIT licensed](https://github.com/bulletproofnetworks/coco/blob/master/LICENSE).
