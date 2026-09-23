# Cross-platform builds and binary size

## Implemented fixes

- Run `go mod tidy` to record the Windows-only dependency
  `golang.zx2c4.com/wireguard/windows v1.0.1` and its checksums.
- Add `-checklinkname=0` only for Android release builds. The pinned
  `github.com/wlynxg/anet` uses private `net` symbols; this matches the pinned
  Xray-core release workflow. It disables the linkname restriction, not TLS or
  certificate verification. This dependency remains sensitive to Go upgrades.
- Use `-mod=readonly` for the main release build and add a build-only CI matrix
  for Linux amd64, Windows amd64/386, and Android arm64. This workflow does not
  publish images, releases, or binaries.
- Remove registration of the standalone core CLI commands: XrayR uses its own
  Cobra entry point, not the core CLI command tree. Proxy/transport/config
  registrations and API services remain unchanged.

## Local verification

Go 1.27.0, CGO disabled, `-trimpath -ldflags '-s -w -buildid='` (plus the
Android-only flag) successfully cross-compiled all four matrix targets.
Certificate merge/renewal unit tests and `TestHysteriaBasicConnectivity`
(plain/Salamander small-packet connectivity) passed. Cross-compilation is not
a Windows/Android runtime test, and these checks do not resolve the separately
documented large-UDP/port-hopping issues in `upgrade-v26.9.9-review.md`.

## Size measurement and proposal — not enabled

Linux amd64 builds with identical flags and the same toolchain/module graph:

| Build | Binary bytes | gzip bytes |
| --- | ---: | ---: |
| XrayR before CLI cleanup | 130101372 | 36104997 |
| XrayR full, after cleanup | 129937532 | 36042043 |
| Experimental Cloudflare + AliDNS only | 58953852 | 19453542 |
| Standalone core (same module graph) | 33800316 | Not measured |

gzip measurements used `gzip -c`; these are not the release ZIP sizes. Compiler
versions and release flags also affect comparisons with downloaded releases.
The experimental build used a temporary Go overlay outside the repository;
it is not a shipping binary or an implemented build profile.

The all-provider lego registry statically links DNS providers and their cloud
SDKs. Limiting that registry in the experimental build saved approximately
54.6% of the binary and 46.0% of gzip size versus the full build. Existing release
flags already strip debugging symbols; simply adding `-s -w` again cannot help.

Recommended follow-up, subject to user review: retain the full build as default
and provide a separately named compact build with Cloudflare and AliDNS DNS-01
providers. Keep all proxy protocols, panels, billing, rate limiting, and
file/HTTP/TLS certificate modes. Reject unsupported DNS providers explicitly
and direct users to the full build. This reduces DNS-provider coverage only;
it must not silently replace the full build or fall back to another provider.
Implement build-profile tests and release artifact naming after approval.
