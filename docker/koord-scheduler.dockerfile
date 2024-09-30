FROM --platform=$TARGETPLATFORM golang:1.20 as builder
WORKDIR /go/src/github.com/koordinator-sh/koordinator

ARG VERSION
ARG TARGETARCH
ENV VERSION $VERSION
ENV GOOS linux
ENV GOARCH $TARGETARCH

COPY go.mod go.mod
COPY go.sum go.sum

# 设置 HTTP 代理
ENV http_proxy=192.168.10.201:7890
ENV https_proxy=192.168.10.201:7890

RUN go mod download

COPY apis/ apis/
COPY cmd/ cmd/
COPY pkg/ pkg/

RUN CGO_ENABLED=0 go build -a -o koord-scheduler cmd/koord-scheduler/main.go

FROM --platform=$TARGETPLATFORM gcr.io/distroless/static:latest
WORKDIR /
COPY --from=builder /go/src/github.com/koordinator-sh/koordinator/koord-scheduler .
ENTRYPOINT ["/koord-scheduler"]
