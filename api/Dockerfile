FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY ../../go.mod ../../go.sum ./
RUN go mod download

COPY ../../ ./

RUN CGO_ENABLED=0 go build -o /server ./controlplane/api/cmd/server

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /server .

EXPOSE 8080

ENTRYPOINT ["/app/server"]
