FROM golang:1.25-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /out/control-agents ./cmd/server

FROM alpine:3.22
RUN adduser -D -h /home/app app
USER app
WORKDIR /home/app
COPY --from=build /out/control-agents /usr/local/bin/control-agents
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/control-agents"]
