# SSPanel-UIM Hysteria2 (`sort = 15`)

XrayR uses Xray-core's native `hysteria` protocol with `version: 2`. The dependency
is official v26.9.9 plus a narrowly scoped TLS certificate snapshot patch from
`HoshinoNeko/Xray-core`, pinned to commit
`a2192dfad2be562ff9b4fa4d4478aa454b6065e1` through `go.mod`'s remote `replace` directive.
This is a patched build, not an unmodified official release. Each
SSPanel user UUID is used as that user's Hysteria2 authentication password.

The v26.9.9 upgrade is not fully production-validated: see
[upgrade review](upgrade-v26.9.9-review.md) for the outstanding UDP and udphop
regressions. The older lifecycle verification results below describe the previous
v26.3.27 patch and do not establish that those new regressions are fixed.

The XrayR node must use `PanelType: SSpanel`. `NodeType` may be left at its
existing value because an SSPanel node with `sort: 15` is negotiated as
`Hysteria2`. TLS is mandatory; configure XrayR `CertConfig` with `file`, `dns`,
`http`, or `tls` mode.

Example SSPanel `custom_config`:

```json
{
  "offset_port_node": "443",
  "offset_port_user": "443",
  "host": "hy.example.com",
  "allow_insecure": false,
  "hysteria2": {
    "version": 2,
    "udpIdleTimeout": 60,
    "masquerade": {
      "type": "404"
    },
    "finalmask": {
      "udp": [
        {
          "type": "salamander",
          "settings": {
            "password": "change-this-obfs-password"
          }
        }
      ],
      "quicParams": {
        "congestion": "bbr",
        "brutalUp": "100 mbps",
        "brutalDown": "100 mbps",
        "initStreamReceiveWindow": 8388608,
        "maxStreamReceiveWindow": 8388608,
        "initConnectionReceiveWindow": 20971520,
        "maxConnectionReceiveWindow": 20971520,
        "maxIdleTimeout": 30,
        "keepAlivePeriod": 10,
        "disablePathMTUDiscovery": false,
        "maxIncomingStreams": 1024
      }
    },
    "portHopping": {
      "enabled": true,
      "autoConfigureFirewall": true,
      "ports": "20000-30000"
    }
  }
}
```

`masquerade` and `finalmask` are passed without a reduced intermediate schema,
so every field supported by Xray-core v26.3.27 remains available. The separate
`portHopping` object controls deployment behavior and subscription output.

When both port-hopping switches are enabled, XrayR first verifies that it is on
Linux, that `iptables` (or `ip6tables` for an IPv6 listen address) is installed, and that the process has permission to
read the NAT table. It then adds idempotent UDP `REDIRECT` rules and removes
them on node disable, configuration replacement, rollback, or shutdown. XrayR
fails the node activation if the preflight or rule installation fails. With
`autoConfigureFirewall: false`, provision equivalent UDP forwarding outside
XrayR.

Subscriptions are generated for Mihomo/Clash, sing-box, Xray JSON, the generic
V2Ray URI list, and the dedicated `/sub/{token}/hysteria2` endpoint. Protocol-
specific SS, SIP002, SIP008, and Trojan formats cannot encode a Hysteria2 node
and continue to return their own supported node types.

## Polling, limits and accounting

Node and user settings are fetched in full every `UpdatePeriodic` seconds.
This is polling, not an immediate push: network failures delay application until
a successful poll. Full fetches intentionally avoid consuming an ETag before
configuration has been applied; they cost more bandwidth than conditional GETs.
Only settings exposed by the panel API and consumed by XrayR affect the daemon.
Subscription-only fields affect subscription output, not a running inbound.

Invalid configurations are rejected before replacing the old node where possible.
Activation failures trigger rollback; if rollback also fails, the node remains
suspended and later polls retry activation. Rebuilding an inbound can disconnect
existing sessions; this is not a seamless migration.

The panel decides whether a user's quota is exhausted. With `keep_connect=false`,
it omits the user; XrayR removes authorization and rejects further I/O. With
`keep_connect=true`, it includes the user with a 1 Mbps (125000 bytes/s) cap.
Dynamic wrappers resolve the current cap, and automatic limits cannot raise the
panel cap. User removal/re-addition does not revive an old stream's authorization.
Node-level quota exhaustion disables the whole node regardless of `keep_connect`.
These actions follow reporting and polling intervals, not an exact local byte cutoff.

Counter baselines survive in-process node reloads and disable/re-enable cycles.
Previously authorized user counters remain tracked for late traffic. Traffic
retries keep the report ID and subtract previously acknowledged batches, including
when the residual batch fails again. This requires the matching SSPanel report-ID
migration. Retry state and baselines are in memory: abrupt process loss is not
covered by a durable accounting journal.

## TLS and client boundaries

Native HY2 inbound TLS advertises ALPN `h3`. Xray-core v26.3.27 no longer permits
`allowInsecure` after its built-in cutoff date. For Xray JSON subscriptions use a
trusted certificate, or set top-level `pinnedPeerCertSha256` in `custom_config` to
the certificate SHA-256 hexadecimal fingerprint. An insecure request without a
pin is rejected by the Xray subscription exporter rather than silently weakening
TLS. Other clients keep their own native TLS options.

Native Xray fields can be passed through to Xray JSON, but other clients cannot
represent every Xray-specific mask or QUIC option. Protocol-specific formats
listed above do not acquire HY2 support merely through subscription conversion.

## Verification and remaining deployment checks

Local regression commands (Go 1.26.1):

```sh
go test ./service/controller -run 'TestHysteriaLifecycleAndTCPForwarding|TestBuild' -count=1 -timeout=45s
go test -race ./api/sspanel -run 'TestTrafficMultiple|TestNodeConfigPort|TestNodeAndUsers|TestKeepConnect|TestGetNodeInfoHysteria2Contract|TestSubtract|TestParseDisabled' -count=1
go test -race ./common/limiter ./app/mydispatcher ./common/porthopping -count=1
go test ./... -run '^$'
go build ./...
go vet ./...
```

Whole-project `go vet` currently reports pre-existing findings in
`api/newV2board/model.go` (unexported JSON field) and `api/pmpanel/pmpanel_test.go`
(unkeyed literals). Run `go vet ./api/sspanel ./app/mydispatcher ./common/limiter
./common/porthopping ./service/controller` to check the affected packages separately.

The lifecycle test uses real native-core TCP and UDP forwarding and checks billing,
disable/recovery, invalid configuration rejection, occupied-port rollback and
reload without historical rebilling. Old `TestController` requires an external
panel and waits for a signal; it is not an unattended unit test.

**TLS race fixed in the pinned fork:** official v26.3.27 had an unsynchronized
certificate slice read/write and in-place OCSP mutation. The fork publishes
immutable certificate snapshots atomically, retains hot reload/OCSP/SNI behavior,
and retries missing or invalid renewal files while retaining the old certificate.
The native HY2 protocol and custom configuration schema are unchanged.

Both the isolated certificate suite and the real HY2 lifecycle race test passed
three consecutive runs with the local patch and again with the pinned remote
dependency. Reproduce with:

```sh
go test -race github.com/xtls/xray-core/transport/internet/tls -run 'TestCertificate|TestExpiredCertificate|TestInsecureCertificates' -count=3 -timeout=90s
go test -race ./service/controller -run '^TestHysteriaLifecycleAndTCPForwarding$' -count=3 -timeout=90s
```

The core TLS package also contains live ECH tests requiring external DNS/HTTPS;
those failed or timed out in this environment and are not marked as passing.
The separate dynamically issuing CA path is outside this ordinary server
certificate hot-reload patch. Fork implementation details are in
`TLS-SNAPSHOT-PATCH.md` at the pinned commit.

Linux NAT installation/removal still requires a disposable Linux test host with
iptables privileges. Real database transactions, migrations and concurrent report
deduplication require SSPanel's database test environment. Passing local contract
tests does not substitute for these deployment checks or real-client testing.
