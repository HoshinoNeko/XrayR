# Hysteria2 UDP integrity regression

XrayR pins the public HoshinoNeko/Xray-core fork to commit
`3a4e3c4fb9833632fe86cf2c1b44c5d68b462099`, based on upstream v26.9.9
and retaining this fork's TLS certificate snapshot fixes. The dependency is a
remote Go pseudo-version, not a local filesystem replacement.

The core fixes QUIC fragment truncation, UDP packet handling above the regular
8192-byte buffer, and the missing receiver in remote-only port hopping. See
[the core implementation notes](https://github.com/HoshinoNeko/Xray-core/blob/3a4e3c4fb9833632fe86cf2c1b44c5d68b462099/docs/hysteria2-udp-datagram-fix.md).

## Run the regression

```sh
go mod download
go test -tags nolego ./service/controller \
  -run '^TestHysteriaLifecycleAndTCPForwarding$' -count=1 -timeout=60s -v
```

The test exercises plain Hysteria2, Salamander, and Salamander + remote-only
port hopping. Each mode covers TCP forwarding, exact UDP payload comparison,
traffic accounting, disable/recovery, and node hot reload/rollback. Payload
bytes vary across the datagram rather than repeating one byte.

Sizes tested are 11, 1200, 4096, 8192, 16000, and 65507 bytes. A native socket
probe detects `EMSGSIZE` before proxy testing: macOS commonly limits UDP writes
to 9216 bytes, so it logs that 16000 and 65507 cannot be tested end to end on
that host. The core's protocol-only fragmentation/reassembly tests cover those
sizes regardless of OS limits. Run the same regression on Linux to validate
large native socket traffic there; the local macOS run is not that evidence.

No authentication, `custom_config`, or `config.yml` changes are required.
The fix increases UDP-only receive capacities; it does not enlarge TCP buffers
or claim a reduction in UDP memory usage.
