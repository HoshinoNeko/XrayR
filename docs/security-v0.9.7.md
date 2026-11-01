# XrayR v0.9.7 security update

The core is pinned to the public **HoshinoNeko/Xray-core v26.10.31** fork commit
`7afd4a49d8d4cc1a06600a88e6c82a30528c2120`, not a local filesystem replacement.
The fork retains the TLS server-certificate snapshot and Hysteria2 UDP integrity
patches that must not be lost when upgrading from the old patched v26.9.9 base.

## MPTCP mitigation is not a kernel fix

Upstream v26.10.31 explicitly disables implicit MPTCP on its TCP listeners. This
also applies to XrayR's SOCKS/HTTP custom inbounds. XrayR's own main module adds
`godebug multipathtcp=0` so other Go listeners share the safer build default.

Check custom `streamSettings.sockopt`: an explicit `tcpMptcp: true` still opts
in and should not be used on an unpatched host. The release test checks that
nil/empty/keepalive socket options remain ordinary TCP even when an environment
requests `GODEBUG=multipathtcp=1`; a deliberate opt-in is separately tested.

**Update the host Linux/Android kernel to the distribution's fixed build for
CVE-2026-98432 and reboot into it.** The underlying IPv4 TCP flaw is not confined
to MPTCP. Disabling MPTCP reduces the demonstrated attack surface; it does not
remove the kernel vulnerability.

## Compatibility

- Keeps UUID authentication, Salamander, port hopping, per-user accounting,
  limits, disable/re-enable and rollback paths.
- Preserves exact Hysteria2 datagrams through 65507 bytes and leaves the normal
  TCP buffer at 8192 bytes. Native large-datagram socket tests require Linux;
  macOS may reject writes above its host UDP limit.
- Includes the three XrayR adaptations for removed core error severity APIs.
- The new core still supports `finalmask.udp` Salamander `settings.password`.
  Its finalmask implementation was refactored: legacy `udphop.settings.sockopt`
  is no longer a recognized inner field; review such custom configurations and
  use the appropriate outer `streamSettings.sockopt` instead.
- The newer core uses built-in TLS trust roots by default. A custom outbound
  relying on an installed system CA must review `tlsSettings.useSystemCA`.

## Editions and release checks

Full, minimal and nolego continue to use `-trimpath -ldflags="-s -w -buildid="`.
Supported full binaries/images retain UPX `--best --lzma`; minimal/nolego do not
include lego/automatic certificate issuance and are not packed. File/none
certificate modes remain available. Extra UPX Actions artifacts are retained.

Tag publication runs full/minimal/nolego checks on Linux amd64, Windows amd64,
Windows 386 and Android arm64. Linux also runs local/mock API, Hysteria2 large
datagram forwarding, accounting/limits, disable/reload/rollback, custom inbound
and firewall tests, protocol-level fragmentation, TLS snapshot race tests,
MPTCP actual-socket controls, and an image smoke test. IPv4 MPTCP control support
is required for the Linux release gate; a local macOS skip is not counted as a
runtime security result.

A draft Release is created only after these checks. It becomes public only
after every release binary and upload succeeds. Existing published releases
and older tags are never overwritten.

References: [upstream security release](https://github.com/XTLS/Xray-core/releases/tag/v26.10.31),
[Go security discussion](https://github.com/golang/go/issues/82717).
