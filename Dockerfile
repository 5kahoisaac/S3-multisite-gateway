# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS builder
WORKDIR /src

# Dependency versions are pinned in go.mod. Download inside the builder only.
COPY go.mod ./
RUN go mod download
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/s3-multisite .

# Distroless provides certificates and a non-root user, with no shell.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /out/s3-multisite /s3-multisite
USER nonroot:nonroot
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/s3-multisite"]
