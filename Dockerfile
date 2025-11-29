FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o replychat ./src

FROM gcr.io/distroless/base-debian12:latest

COPY --from=builder /app/replychat /usr/local/bin/replychat

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/replychat"]
