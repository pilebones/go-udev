## Build
# Keep this version in sync with the go directive of go.mod
FROM golang:1.24-alpine AS builder
WORKDIR /build
# go.sum is optional, the library has no dependency
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /go-udev ./cmd/go-udev

## Deploy
FROM alpine:3.21
# go-udev only needs to read /sys and to receive netlink uevents, both allowed for unprivileged users
RUN adduser -S -H -u 10001 go-udev
COPY --from=builder /go-udev /go-udev
USER 10001
ENTRYPOINT ["/go-udev"]
