FROM golang:1.24-alpine

COPY bitrise-build-cache-cli /usr/local/bin/bitrise-build-cache-cli

ENTRYPOINT ["/usr/local/bin/bitrise-build-cache-cli"]
