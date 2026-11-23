# syntax=docker/dockerfile:1
FROM node:22-bookworm-slim AS frontend
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY scripts/copy-assets.mjs scripts/
COPY web/static/ web/static/
COPY web/templates/ web/templates/
RUN mkdir -p web/static/dist && npm run build

FROM golang:1.26.11-bookworm AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY Makefile sqlc.yaml ./
COPY cmd/ cmd/
COPY db/ db/
COPY internal/ internal/
COPY web/ web/
COPY --from=frontend /src/web/static/dist/ web/static/dist/
RUN make generate \
    && go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w" -o /out/ ./cmd/web ./cmd/worker ./cmd/migrate ./cmd/container-health ./cmd/marketplace-admin

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/ /app/
USER 65532:65532
EXPOSE 8080
CMD ["/app/web"]
