
FROM golang:1.15.2-alpine as build
ARG GOARCH=
ARG GO_BUILD_ARGS=

RUN mkdir /build
WORKDIR /build
COPY . .
RUN  go build -v $GO_BUILD_ARGS -o /build/picopublish .

FROM alpine
WORKDIR /app
COPY --from=build /build/static /app/static
COPY --from=build /build/picopublish /app/picopublish
RUN chmod +x /app/picopublish
ENTRYPOINT ["/app/picopublish"]
