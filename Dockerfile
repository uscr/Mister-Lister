FROM golang:1.21 AS builder
WORKDIR /src
COPY *.go go.mod go.sum ./
COPY webapp/ ./webapp/
RUN go build -o /bin/misterlister -ldflags="-extldflags=-static"

FROM scratch
COPY --from=builder /bin/misterlister /bin/misterlister
COPY --from=builder /src/webapp /webapp
CMD ["/bin/misterlister"]