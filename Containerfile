# This is a server-only image. It cannot manage host SSH/tmux sessions unless a
# future deployment explicitly shares the Unix user, process/session namespaces,
# lifecycle state, tmux socket, and ttyd runtime dependencies.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /out/control-agents-server ./cmd/server

FROM alpine:3.22
LABEL org.opencontainers.image.description="Server-only Control Agents image; host managed-session lifecycle integration is not included"
RUN adduser -D -h /home/app app
USER app
WORKDIR /home/app
COPY --from=build /out/control-agents-server /usr/local/bin/control-agents-server
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/control-agents-server"]
