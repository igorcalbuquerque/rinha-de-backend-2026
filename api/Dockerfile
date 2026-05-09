FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY api/go.mod .
RUN go mod download
COPY api/*.go .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o api .

FROM scratch
COPY --from=builder /app/api /api
COPY resources /resources
ENTRYPOINT ["/api"]
