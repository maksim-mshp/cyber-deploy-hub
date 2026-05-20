# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/api-gateway ./cmd/api-gateway
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/core-service ./cmd/core-service
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22

RUN addgroup -S app && adduser -S -G app -u 10001 app
RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=build /out/api-gateway /app/api-gateway
COPY --from=build /out/core-service /app/core-service
COPY --from=build /out/migrate /app/migrate
COPY migrations /app/migrations

USER app

EXPOSE 8080

ENTRYPOINT ["/app/api-gateway"]
