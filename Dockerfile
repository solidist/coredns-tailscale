FROM golang:1.24.4-alpine3.21 AS build
ARG COREDNS_VERSION=1.11.3

WORKDIR /go/src/coredns

RUN apk add git make && \
    git clone --depth 1 --branch=v${COREDNS_VERSION} https://github.com/coredns/coredns /go/src/coredns && cd plugin

# Copy the tailscale plugin
COPY . /go/src/coredns/plugin/tailscale

# Install the tailscale plugin
RUN cd plugin && \
    rm tailscale/go.mod tailscale/go.sum &&  \
    sed -i s/forward:forward/tailscale:tailscale\\nforward:forward/ /go/src/coredns/plugin.cfg && \
    cat /go/src/coredns/plugin.cfg && \
    cd .. && \
    make check && \
    go build

FROM alpine:3.21.3
RUN apk add --no-cache ca-certificates && \
    addgroup -g 1000 coredns && \
    adduser -D -u 1000 -G coredns coredns

# Copy the built binary from the build stage and default configuration files
COPY --from=build /go/src/coredns/coredns /usr/local/bin/
COPY Corefile /etc/coredns/

# Set proper permissions
RUN chown -R coredns:coredns /etc/coredns

USER coredns

WORKDIR /etc/coredns
ENTRYPOINT ["coredns"]
CMD ["-conf", "/etc/coredns/Corefile"]
