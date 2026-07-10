# Multi-stage build: a pure-Go binary, no cgo, so this also validates the
# single-static-binary promise (v0.4 §2) as part of every image build.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /verdict ./cmd/verdict

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /verdict /usr/local/bin/verdict
WORKDIR /data
EXPOSE 8787
ENTRYPOINT ["verdict"]
CMD ["serve", "--host", "0.0.0.0", "--report", "/data/verdict-report"]
