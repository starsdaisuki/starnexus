FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY server/go.mod server/go.sum ./server/
COPY server/ ./server/
RUN cd server && CGO_ENABLED=0 go build -trimpath -o /out/starnexus-server .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S starnexus && adduser -S -G starnexus starnexus
WORKDIR /app
COPY --from=builder /out/starnexus-server /app/starnexus-server
COPY server/schema.sql /app/schema.sql
COPY web /app/web
COPY server/config.yaml.example /app/config.yaml
RUN mkdir -p /app/data && chown -R starnexus:starnexus /app
USER starnexus
ENV TZ=UTC
# Declare /app/data as a volume so `docker run` without a bind mount
# still gets a named volume rather than losing the database on
# container restart. docker-compose.yml already mounts a named volume
# explicitly; this declaration matters for bare `docker run` users.
VOLUME ["/app/data"]
EXPOSE 8900
CMD ["/app/starnexus-server", "/app/config.yaml"]
