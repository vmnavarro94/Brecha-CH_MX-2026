# latency-tracking Specification

## Purpose

Expose engine detection latency (p50/p99 in µs) in real time so observers can verify
sub-millisecond ProcessUpdate performance without instrumenting the process themselves.

## Requirements

### Requirement L1: Ring-buffer latency recording

The system MUST record the elapsed time of every ProcessUpdate call in a
fixed-size 1024-slot ring buffer of int64 nanoseconds. When all 1024 slots are
filled, subsequent writes MUST evict the oldest sample (circular overwrite).
LatencyTracker.Record MUST be called exactly once per ProcessUpdate invocation,
capturing T1 at entry and T2 just before return.

#### Scenario: Single update recorded

- GIVEN the LatencyTracker is freshly created (count = 0)
- WHEN one ProcessUpdate call completes
- THEN Stats() returns samples = 1 and p50 > 0

#### Scenario: Ring wraps at 1024

- GIVEN 1024 samples have been recorded
- WHEN one additional sample is recorded
- THEN the ring overwrites slot 0, samples remains 1024, and the oldest value is gone

#### Scenario: Cold-start suppression

- GIVEN fewer than 10 samples have been recorded
- WHEN Stats() is called
- THEN it returns p50 = 0, p99 = 0, samples < 10

### Requirement L2: Exact percentile math

The system MUST compute p50 and p99 by copying the active window into a scratch
slice, sorting ascending, and indexing sorted[n/2] (p50) and sorted[n*99/100] (p99)
where n is the number of valid samples (up to 1024). Computation MUST NOT happen
on the hot path — it MUST occur only when Stats() is called.

#### Scenario: Known ordered input

- GIVEN the ring contains exactly the values [1ns, 2ns, ..., 1024ns] in order
- WHEN Stats() is called
- THEN p50 = 512ns AND p99 = 1014ns

#### Scenario: Uniform input

- GIVEN all 1024 slots contain the same value V
- WHEN Stats() is called
- THEN p50 = V AND p99 = V

### Requirement L3: WS latency_stats event

The system MUST publish a `latency_stats` WebSocket event at most once per second
from the engine processing loop. The event payload MUST conform to:
`{ type: "latency_stats", data: { p50_us: number, p99_us: number, samples: number } }`.
The hub MUST throttle this event type at a minimum interval of 250ms.

#### Scenario: Event fires after warm-up

- GIVEN the bot has been running for at least 5 seconds in demo mode
- WHEN a connected frontend is present
- THEN at least one `latency_stats` event is received per second with p50_us > 0

#### Scenario: Payload shape

- GIVEN a `latency_stats` event is received
- WHEN the payload is inspected
- THEN it contains numeric fields p50_us, p99_us, and samples at the top level of data

#### Scenario: Hub throttle enforced

- GIVEN two latency_stats publishes occur within 250ms
- WHEN the hub dispatches to clients
- THEN only one event is delivered to each client in that window

### Requirement L4: StatusBar latency display

The StatusBar MUST render the block `detect p50: X.Xµs | p99: Y.Yµs` when
`latency.samples >= 10` in the frontend store. The block MUST be hidden when
`latency.samples < 10`. Values MUST be formatted to one decimal place in µs.

#### Scenario: Samples sufficient

- GIVEN the store has latency.samples = 10 or more
- WHEN the StatusBar renders
- THEN the block "detect p50: X.Xµs | p99: Y.Yµs" is visible

#### Scenario: Cold start hidden

- GIVEN the store has latency.samples < 10 (including 0)
- WHEN the StatusBar renders
- THEN no latency block is visible

#### Scenario: Value freshness

- GIVEN p50 changes in the store
- WHEN at most 2 seconds elapse
- THEN the StatusBar reflects the updated value

### Requirement L5: Zero added dependencies

After implementation, the project MUST have no new entries in `go.mod` and no new
entries in `package.json` compared to the pre-change baseline.

#### Scenario: go.mod unchanged

- GIVEN the implementation is complete
- WHEN `go.mod` is diffed against the pre-change version
- THEN no new `require` lines are present

#### Scenario: package.json unchanged

- GIVEN the implementation is complete
- WHEN `package.json` is diffed against the pre-change version
- THEN no new dependency or devDependency entries are present

### Requirement L6: Concurrency-safe Stats accessor

LatencyTracker.Stats() and LatencyTracker.Record() MUST be safe to call from
different goroutines concurrently. The implementation MUST use sync.RWMutex:
Record holds a write lock; Stats holds a read lock.

#### Scenario: No data race under concurrent access

- GIVEN Record() is called continuously in one goroutine
- WHEN Stats() is called from a different goroutine simultaneously
- THEN the Go race detector reports no data race
