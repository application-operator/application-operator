FROM golang:1.14.4-alpine3.12 AS builder
RUN apk update && apk add --no-cache musl-dev gcc build-base
WORKDIR /src
COPY go.mod go.sum /src/
RUN go mod download
COPY . /src
RUN go build ./... && go test ./... && go install ./...

FROM alpine:3.12
COPY --from=builder /go/bin/manager /usr/local/bin/application-operator

