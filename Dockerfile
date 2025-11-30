FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
COPY go-llama.cpp ./go-llama.cpp
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o replychat ./src

FROM debian:bookworm-slim AS runner

RUN apt-get update && apt-get install -y \
    ca-certificates \
    git \
    openssh-client \
    gosu \
    && rm -rf /var/lib/apt/lists/*

RUN git config --system user.name "ReplyChat Agent" && \
    git config --system user.email "agent@replychat.local"

WORKDIR /app

COPY --from=builder /app/replychat /usr/local/bin/replychat
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

RUN useradd -m replychat && \
    mkdir -p /data/projects && \
    chown -R replychat:replychat /data

EXPOSE 8080

VOLUME ["/app/data"]

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
