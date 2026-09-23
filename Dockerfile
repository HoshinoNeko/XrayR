# Build go
FROM --platform=$BUILDPLATFORM golang:1.27.0-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG EDITION=full
WORKDIR /app
COPY . .
ENV CGO_ENABLED=0
RUN go mod download
RUN case "$EDITION" in full) tags="" ;; minimal|nolego) tags="$EDITION" ;; *) exit 1 ;; esac; \
    GOOS="$TARGETOS" GOARCH="$TARGETARCH" GOARM="${TARGETVARIANT#v}" \
    go build -mod=readonly -tags "$tags" -v -o XrayR -trimpath -ldflags "-s -w -buildid="
RUN if [ "$EDITION" = full ]; then apk add --no-cache upx && sh .github/build/pack-full.sh XrayR; fi

# Release
FROM  alpine
ENV GOMEMLIMIT=40MiB
# 安装必要的工具包
RUN  apk --update --no-cache add tzdata ca-certificates \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
RUN mkdir /etc/XrayR/
COPY --from=builder /app/XrayR /usr/local/bin

ENTRYPOINT [ "XrayR", "--config", "/etc/XrayR/config.yml"]
