FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o app .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/app .
EXPOSE 9999
CMD ["./app"]
