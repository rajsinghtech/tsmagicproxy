# Copyright (c) Tailscale Inc & AUTHORS
# SPDX-License-Identifier: BSD-3-Clause

# Note that this Dockerfile is currently NOT used to build any of the published
# Tailscale container images and may have drifted from the image build mechanism
# we use.
# Tailscale images are currently built using https://github.com/tailscale/mkctr,
# and the build script can be found in ./build_docker.sh.
#
#
# This Dockerfile includes all the tailscale binaries.
#
# To build the Dockerfile:
#
#     $ docker build -t tailscale/tailscale .
#
# To run the tailscaled agent:
#
#     $ docker run -d --name=tailscaled -v /var/lib:/var/lib -v /dev/net/tun:/dev/net/tun --network=host --privileged tailscale/tailscale tailscaled
#
# To then log in:
#
#     $ docker exec tailscaled tailscale up
#
# To see status:
#
#     $ docker exec tailscaled tailscale status


FROM golang:1.24-alpine AS builder

# Install required system dependencies
RUN apk add --no-cache gcc musl-dev

# Set the working directory
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY *.go ./

# Build the application
RUN CGO_ENABLED=1 go build -o tsmagicproxy .

# Create a minimal runtime image
FROM alpine:latest

# Install required runtime dependencies
RUN apk add --no-cache ca-certificates

# Copy the binary from the builder stage
COPY --from=builder /app/tsmagicproxy /usr/local/bin/tsmagicproxy

# Create a directory for state
RUN mkdir -p /var/lib/tsmagicproxy && chmod 700 /var/lib/tsmagicproxy

# Expose port 53 for DNS
EXPOSE 53/udp

# Set environment variables
ENV TS_STATE_DIR=/var/lib/tsmagicproxy
ENV TSNET_FORCE_LOGIN=1

# Run the application
ENTRYPOINT ["tsmagicproxy", "-state-dir", "/var/lib/tsmagicproxy"]

# By default, listen on all interfaces
CMD ["-listen", "0.0.0.0:53"]
