# ModernChat 后端镜像（多阶段构建）
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/modernchat-server ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=build /out/modernchat-server .
COPY --from=build /src/migrations ./migrations

EXPOSE 8080
ENTRYPOINT ["./modernchat-server"]
