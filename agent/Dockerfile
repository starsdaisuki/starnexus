FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY agent/go.mod agent/go.sum ./agent/
COPY agent/ ./agent/
RUN cd agent && CGO_ENABLED=0 go build -trimpath -o /out/starnexus-agent .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S starnexus && adduser -S -G starnexus starnexus
WORKDIR /app
COPY --from=builder /out/starnexus-agent /app/starnexus-agent
COPY agent/config.yaml.example /app/config.yaml
USER starnexus
ENV TZ=UTC
CMD ["/app/starnexus-agent", "/app/config.yaml"]
