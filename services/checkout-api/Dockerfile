# syntax=docker/dockerfile:1.9

ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-alpine AS build

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w \
        -X github.com/meridian-platform/meridian/services/checkout-api/internal/version.Version=${VERSION} \
        -X github.com/meridian-platform/meridian/services/checkout-api/internal/version.Commit=${COMMIT} \
        -X github.com/meridian-platform/meridian/services/checkout-api/internal/version.BuildDate=${BUILD_DATE}" \
      -o /out/checkout-api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev
ARG COMMIT=none

LABEL org.opencontainers.image.title="checkout-api" \
      org.opencontainers.image.description="Meridian checkout API" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/your-org/meridian"

COPY --from=build /out/checkout-api /usr/local/bin/checkout-api

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/checkout-api"]
