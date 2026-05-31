# syntax=docker/dockerfile:1.7

# ---- Stage 1: build Go binary (statically linked) ----------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=docker
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/roomkit ./cmd/roomkit

# ---- Stage 2: alpine runtime (needs ffmpeg for recording mux) ----------------
FROM alpine:3.20
RUN apk add --no-cache ffmpeg ca-certificates tzdata \
    && addgroup -S roomkit \
    && adduser -S -G roomkit -u 10001 roomkit \
    && mkdir -p /var/lib/roomkit/recordings \
    && chown -R roomkit:roomkit /var/lib/roomkit
COPY --from=build /out/roomkit /usr/local/bin/roomkit
USER roomkit
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/roomkit"]
CMD ["--addr", ":8080"]
