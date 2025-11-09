# Build stage
FROM golang:1.22-alpine AS build

WORKDIR /app

RUN apk add --no-cache git

ENV GOTOOLCHAIN=go1.25.0

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o brancr ./cmd/brancr

# Runtime stage
FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=build /app/brancr ./brancr
COPY --from=build /app/config.yml ./config.yml
COPY --from=build /app/internal/admin/templates ./internal/admin/templates
COPY --from=build /app/static ./static

EXPOSE 8080

ENV PORT=8080

CMD ["./brancr"]

