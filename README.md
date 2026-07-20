# Go Loadgen

Go Loadgen is a protocol-agnostic, open-loop load generator for Go. Applications provide typed endpoint adapters; the library schedules offered load, distributes it across endpoints, and waits for issued requests to finish.

## Semantics

- A phase's `RPS` is its total offered rate across all targets.
- Scheduling is open-loop: response latency never controls future arrivals.
- Endpoint selection is compiled before a run and uses O(1), lock-free weighted selection.
- `Run` stops issuing requests at phase boundaries and waits for in-flight requests by default.
- `DrainTimeout` is optional. When set, outstanding requests are cancelled after that period; arrivals are never blocked.
- `MaxInFlight` is optional. When full, new arrivals are dropped and reported, preserving open-loop semantics. Loader delays are reported as missed rather than replayed as a catch-up burst.

## Scheduling Accuracy And Throughput

`RPS` is an offered-load target, not a guarantee that every scheduled arrival is issued. The scheduler uses 1 ms batches at rates of 1,000 RPS and above. If the loader is delayed by OS scheduling, Go runtime work, garbage collection, request goroutine creation, client-side serialization, or transport work, it can miss a batch deadline.

Go Loadgen intentionally does not replay overdue batches. Catching up would create a burst above the configured instantaneous rate, retain more work in memory, and hide loader saturation. Instead, the report records those arrivals in `Missed`; they were never sent. `Dropped` has a different meaning: an arrival was timely but rejected because `MaxInFlight` was full.

This is an explicit performance and measurement trade-off. The scheduling hot path avoids queues and blocking, and the report exposes whether the generator kept up. Benchmarks should report `Scheduled`, `Issued`, `Missed`, `Dropped`, and `Completed`, rather than treating configured RPS as achieved RPS.

## Example

```go
collector, err := go_loadgen.NewGobCollector[Result]("results.gob", time.Second)
if err != nil {
    log.Fatal(err)
}
defer collector.Close()

api, err := go_loadgen.NewEndpoint(client, provider, collector)
if err != nil {
    log.Fatal(err)
}
workload, err := go_loadgen.NewWorkload(go_loadgen.Spec{
    Duration: 60 * time.Second,
    Seed:     42,
    Endpoints: map[string]go_loadgen.Endpoint{
        "api": api,
    },
    Phases: []go_loadgen.Phase{
        {
            Duration: 60 * time.Second,
            RPS:      10_000,
            Targets:  []go_loadgen.Target{{Endpoint: "api", Weight: 1}},
        },
    },
})
if err != nil {
    log.Fatal(err)
}

report := workload.Run(context.Background())
log.Printf("scheduled=%d issued=%d dropped=%d missed=%d completed=%d", report.Scheduled, report.Issued, report.Dropped, report.Missed, report.Completed)
```

`Client`, `DataProvider`, and `Collector` implementations are called concurrently. Clients should reuse connections and honor their supplied context. For high result volume, prefer `GobCollector`; CSV conversion and its writer lock are deliberately not the low-overhead path.

## Multi-Endpoint Workloads

Register every endpoint once, then split each phase's aggregate rate with integer weights:

```go
Endpoints: map[string]go_loadgen.Endpoint{"read": readEndpoint, "write": writeEndpoint},
Phases: []go_loadgen.Phase{{
    Duration: time.Minute,
    RPS:      50_000,
    Targets: []go_loadgen.Target{
        {Endpoint: "read", Weight: 80},
        {Endpoint: "write", Weight: 20},
    },
}},
```

Each phase has its own deterministic random stream derived from `Spec.Seed`; no map lookup, mutex, or floating-point calculation occurs while choosing an endpoint.

## License

Apache License 2.0. See [LICENSE](LICENSE).
