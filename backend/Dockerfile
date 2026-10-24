# syntax=docker/dockerfile:1.7
# Çok aşamalı derleme: tek derleme aşaması üç ikiliyi üretir, her servis yalnız kendi ikilisini içeren
# distroless (kabuk yok, paket yöneticisi yok) imajda root olmayan kullanıcıyla çalışır.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/simulator ./cmd/simulator && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/verify ./cmd/verify

FROM gcr.io/distroless/static-debian12:nonroot AS api
COPY --from=build /out/api /app/api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/api"]

FROM gcr.io/distroless/static-debian12:nonroot AS simulator
COPY --from=build /out/simulator /app/simulator
USER nonroot:nonroot
ENTRYPOINT ["/app/simulator"]

FROM gcr.io/distroless/static-debian12:nonroot AS verify
COPY --from=build /out/verify /app/verify
USER nonroot:nonroot
ENTRYPOINT ["/app/verify"]
