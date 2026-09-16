FROM golang:1.27.1-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/nguyenduytan/proxysieve/internal/buildinfo.Version=${VERSION} -X github.com/nguyenduytan/proxysieve/internal/buildinfo.Commit=${COMMIT} -X github.com/nguyenduytan/proxysieve/internal/buildinfo.Date=${BUILD_DATE}" -o /out/proxysieve ./cmd/proxysieve

FROM scratch
LABEL org.opencontainers.image.title="ProxySieve" \
      org.opencontainers.image.authors="Tony Nguyen" \
      org.opencontainers.image.source="https://github.com/nguyenduytan/proxysieve" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /out/proxysieve /proxysieve
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY LICENSE NOTICE /licenses/
USER 65532:65532
ENTRYPOINT ["/proxysieve"]
CMD ["version"]
