# Traffic Accounting Foundation

Current gateway events count successful HTTP application-stream reads/writes at
ProxySieve's client/upstream boundaries. Partial I/O that returns an error still
adds its successful byte count. The event includes client upload/download, upstream
upload/download, direct bytes, health-check bytes and estimated avoided bytes as
separate values.

These numbers are **not** a NIC, TCP/IP, TLS or provider-billing meter. Provider
framing, rounding and measurement points may differ. Costs use fixed-point currency
and explicit decimal GB/GiB rate snapshots; no floating-point value is authoritative.

Live events are presently bounded in memory and authenticated through the local API.
Tunnel/SOCKS accounting, durable event retention, minute/hour/day rollups, budget
reservations, projections and configured-cost analytics remain later milestones.
The dashboard must label estimated avoided bytes as an estimate at every display.
