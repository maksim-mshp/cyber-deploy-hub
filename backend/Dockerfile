# syntax=docker/dockerfile:1

FROM golang:1.25.10-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/api-gateway ./cmd/api-gateway
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/core-service ./cmd/core-service
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/project-pool-service ./cmd/project-pool-service
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/capacity-service ./cmd/capacity-service
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/cloud-adapter-service ./cmd/cloud-adapter-service
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/vdi-gateway-service ./cmd/vdi-gateway-service
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/lifecycle-service ./cmd/lifecycle-service
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/checker-service ./cmd/checker-service
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/lms-gateway-service ./cmd/lms-gateway-service
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22

RUN addgroup -S app && adduser -S -G app -u 10001 app
RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=build /out/api-gateway /app/api-gateway
COPY --from=build /out/core-service /app/core-service
COPY --from=build /out/project-pool-service /app/project-pool-service
COPY --from=build /out/capacity-service /app/capacity-service
COPY --from=build /out/cloud-adapter-service /app/cloud-adapter-service
COPY --from=build /out/vdi-gateway-service /app/vdi-gateway-service
COPY --from=build /out/lifecycle-service /app/lifecycle-service
COPY --from=build /out/checker-service /app/checker-service
COPY --from=build /out/lms-gateway-service /app/lms-gateway-service
COPY --from=build /out/migrate /app/migrate
COPY config /app/config
COPY migrations /app/migrations

USER app

EXPOSE 8080

ENTRYPOINT ["/app/api-gateway"]
