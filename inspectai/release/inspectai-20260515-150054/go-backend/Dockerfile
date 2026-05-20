FROM golang:1.24-alpine AS build

WORKDIR /src/go-backend
COPY go-backend/go.mod ./
COPY go-backend/ ./
RUN GOTELEMETRY=off go build -o /out/inspectai ./cmd/server

FROM alpine:3.20

WORKDIR /app
COPY --from=build /out/inspectai /app/inspectai
COPY frontend/ /app/frontend/

EXPOSE 8080
CMD ["/app/inspectai"]
