FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/projects-service ./cmd/service

FROM gcr.io/distroless/static-debian12

WORKDIR /app

COPY --from=builder /bin/projects-service /app/projects-service

EXPOSE 8000

ENTRYPOINT ["/app/projects-service"]
