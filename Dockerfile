FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY . .

RUN go build -o bitrise-build-cache-cli

FROM alpine:latest

COPY --from=builder /app/bitrise-build-cache-cli /usr/local/bin/bitrise-build-cache-cli

ENTRYPOINT ["/usr/local/bin/bitrise-build-cache-cli"]
