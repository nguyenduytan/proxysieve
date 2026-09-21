# Benchmarking and load smoke

Run the reproducible hot-path suite with:

```sh
make benchmark
```

It records policy evaluation at 100/1,000/10,000 rules, selection at 100/1,000
endpoints, a full traffic-recorder buffer and a response-cache hit. The hosted
Benchmark workflow runs five samples and retains `benchmark.txt` for 30 days.
Compare the same benchmark, Go version, runner class and commit; one workstation
sample is not a release claim.

Run the bounded correctness-under-load gate with:

```sh
make load
```

It exercises 512 concurrent HTTP proxy requests through local sockets, a 1 MB
bidirectional CONNECT stream and an 8,000-event durable traffic burst. It uses no
public host or paid proxy and deliberately has no wall-clock threshold. Timing
thresholds belong to accepted RC baselines, not correctness tests.

Before promoting an RC, archive the hosted five-run result, investigate material
regressions against that RC baseline, and describe only measurements the fixture
actually observes. Application-stream bytes are not NIC bytes or a provider bill;
see [traffic accounting](traffic-accounting.md).

