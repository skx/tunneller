FROM golang:1.12-alpine as builder

RUN apk update && apk add --no-cache git ca-certificates && update-ca-certificates

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /go/src/github.com/skx/tunneller/

COPY . .

RUN GO111MODULE=on go mod download
RUN GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-w -extldflags "static"'''



FROM scratch


WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd

USER appuser

COPY --from=builder /go/src/github.com/skx/tunneller/tunneller .

ENTRYPOINT ["/app/tunneller"]
CMD ["serve", "-host", "0.0.0.0"]
