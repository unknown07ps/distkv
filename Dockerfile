FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN go build -o /node ./cmd/node

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /node /node
RUN mkdir -p /data
VOLUME ["/data"]
ENTRYPOINT ["/node"]
