# Build stage
FROM golang:1.23 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . /app
RUN CGO_ENABLED=0 go build -o /app/eve-chainkills

# Run stage
FROM alpine:3.17
WORKDIR /app
COPY --from=builder /app/eve-chainkills /app/
COPY config.json /app/

# The container will run the compiled Go binary
ENTRYPOINT ["/app/eve-chainkills"]
