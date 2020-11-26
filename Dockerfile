FROM golang:1.15 AS builder
WORKDIR /go/src/app

COPY go.mod .
COPY go.sum .
RUN go get github.com/kyleconroy/sqlc/cmd/sqlc

COPY main.go .
RUN go build -o sqlc-playground .

COPY index.tmpl.html .
COPY static static
COPY go/src/sqlc.dev/docs go/src/sqlc.dev/docs

FROM heroku/heroku:20
WORKDIR /app
COPY --from=builder /go/bin/sqlc .
COPY --from=builder /go/src/app .
CMD ["/app/sqlc-playground", "/app/go", "/app/sqlc"]
