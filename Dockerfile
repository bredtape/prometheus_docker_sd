FROM golang:1.21.0

ENV CGO_ENABLED=0

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o main .

FROM scratch

WORKDIR /app

COPY --from=0 /app/main /app/main
COPY --from=0 /app/web/*.html /app/web/
COPY --from=0 /app/web/static/* /app/web/static/

EXPOSE 9200

ENTRYPOINT ["/app/main"]
