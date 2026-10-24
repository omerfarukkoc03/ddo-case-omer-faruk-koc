# syntax=docker/dockerfile:1.7
# Çok aşamalı derleme: tek derleme aşaması üç ikiliyi üretir, her servis yalnız kendi ikilisini içeren
# distroless (kabuk yok, paket yöneticisi yok) imajda root olmayan kullanıcıyla çalışır.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY . .
ARG VERSION=dev
# `go mod download` kullanılmaz: go.mod'daki tüm modülleri (testcontainers gibi yalnız testte kullanılanlar dahil)
# indirirdi. go build yalnız ikililerin gerçekten içe aktardığı modülleri indirir; üç ikili tek çağrıda paralel derlenir.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/ ./cmd/api ./cmd/simulator ./cmd/verify

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
