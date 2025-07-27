FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o app .

FROM alpine:latest
RUN apk --no-cache add wget
WORKDIR /app
COPY --from=builder /app/app .
COPY healthcheck.sh .
RUN chmod +x healthcheck.sh
EXPOSE 9999
CMD ["./app"]
