
FROM golang:1.15.2-alpine as build
ARG GOARCH=
ARG GO_BUILD_ARGS=

RUN mkdir /build
WORKDIR /build
COPY go.mod /build/go.mod
COPY go.sum /build/go.sum
COPY main.go /build/main.go
RUN  go build -v $GO_BUILD_ARGS -o /build/picopublish main.go

FROM alpine
WORKDIR /app
COPY --from=build /build/picopublish /app/picopublish
COPY ./static /app/static
COPY ./index.html /app/index.html
RUN chmod +x /app/picopublish
ENTRYPOINT ["/app/picopublish"]
