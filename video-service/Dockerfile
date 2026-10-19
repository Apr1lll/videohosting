FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# -p 1: компилировать по одному пакету — меньше памяти (важно для VPS с 1–2 ГБ без swap)
RUN CGO_ENABLED=0 go build -p 1 -ldflags="-s -w" -o /bin/app ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /bin/app /bin/app
RUN mkdir -p /data/videos
EXPOSE 8082
ENTRYPOINT ["/bin/app"]
