FROM golang:1.25-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /out/control-agents-server ./cmd/server

FROM alpine:3.22
RUN adduser -D -h /home/app app
USER app
WORKDIR /home/app
COPY --from=build /out/control-agents-server /usr/local/bin/control-agents-server
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/control-agents-server"]
