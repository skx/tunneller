FROM golang:1.12 as builder
WORKDIR /go/src/github.com/skx/tunneller/

COPY . .

RUN CGO_ENABLED=0 GO111MODULE=on go build -ldflags '-w -extldflags "-static"'

FROM scratch

WORKDIR /app
COPY --from=builder /go/src/github.com/skx/tunneller/tunneller .

ENTRYPOINT ["/app/tunneller"]
CMD ["serve"]
