ARG ARCH=

FROM ${ARCH}golang:1.22.2 as builder

WORKDIR /app

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY ./ ./

RUN go test ./...

RUN GOOS=linux CGO_ENABLED=0 go build

FROM ${ARCH}alpine:3.16.2

COPY --from=builder /app/openevse-statsd /bin/openevse-statsd

CMD ["/bin/openevse-statsd","-configFile=/var/lib/openevse-statsd/config.json"]
