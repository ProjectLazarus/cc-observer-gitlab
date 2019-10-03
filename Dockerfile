FROM golang:1.12-alpine as builder

WORKDIR /go/src/cc-observer-gitlab

RUN apk update && apk add ca-certificates && \
    apk add git && apk add gcc && apk add libc-dev \
    && rm -rf /var/cache/apk/*

COPY src .
RUN go get -v -d && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix cgo -o main

FROM scratch

WORKDIR /

COPY --from=builder /etc/ssl/certs/ca-certificates.crt \
  /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /go/src/cc-observer-gitlab/main .
CMD ["/main"]
