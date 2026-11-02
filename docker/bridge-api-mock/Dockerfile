FROM golang:1.27.3-alpine3.23 AS builder
WORKDIR /src
RUN printf 'module apimock\n\ngo 1.27.3\n' > go.mod
COPY docker/bridge-api-mock/main.go ./main.go
RUN CGO_ENABLED=0 go build -o /bridge-api-mock .

FROM alpine:3.24.3
RUN apk add --no-cache ca-certificates
COPY --from=builder /bridge-api-mock /usr/local/bin/bridge-api-mock
EXPOSE 8443
ENTRYPOINT ["/usr/local/bin/bridge-api-mock"]
