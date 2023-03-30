FROM golang:1.20.2

ENV CGO_ENABLED=0

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY *.go ./

RUN go build -o main .


FROM scratch

WORKDIR /app

COPY --from=0 /app/main /app/main

ENTRYPOINT ["/app/main"]