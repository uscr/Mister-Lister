FROM golang:1.21 as builder
WORKDIR /src
COPY * .
RUN go build -o /bin/misterlister -ldflags="-extldflags=-static"

FROM scratch
COPY --from=builder /bin/misterlister /bin/misterlister
CMD ["/bin/misterlister"]