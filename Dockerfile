FROM golang:1.26-alpine AS build

COPY . /build
RUN apk update && apk add --no-cache make git
WORKDIR /build

RUN make build

FROM alpine:latest

COPY --from=build /build/bin/telegram-llm-bot /usr/bin/telegram-llm-bot

RUN apk add --no-cache ca-certificates bash tmux

ENTRYPOINT ["/usr/bin/telegram-llm-bot"]
