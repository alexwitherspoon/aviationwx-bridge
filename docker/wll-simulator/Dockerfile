FROM golang:1.27.3-alpine3.23 AS builder
WORKDIR /src
RUN printf 'module wllsim\n\ngo 1.27.3\n' > go.mod
COPY docker/wll-simulator/main.go ./main.go
RUN CGO_ENABLED=0 go build -o /wll-simulator .

FROM alpine:3.24.3
RUN apk add --no-cache ca-certificates
COPY --from=builder /wll-simulator /usr/local/bin/wll-simulator
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/wll-simulator"]
