FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build the optimized binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o main .

FROM alpine:latest
RUN apk --no-cache add wget ca-certificates
WORKDIR /app

# Copy the binary
COPY --from=builder /app/main .
COPY healthcheck.sh .

RUN chmod +x healthcheck.sh
EXPOSE 9999

CMD ["./main"]
