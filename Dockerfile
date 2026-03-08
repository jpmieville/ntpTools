# multi-stage build

# Stage 1: build
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o ntpweb ./cmd/ntpweb

# Stage 2: minimal runtime image
FROM scratch

COPY --from=builder /app/ntpweb /ntpweb

EXPOSE 8080

ENTRYPOINT ["/ntpweb"]
CMD ["-addr", ":8080"]
