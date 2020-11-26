FROM golang:1.15 AS builder
WORKDIR /go/src/app
COPY . .
RUN go get github.com/kyleconroy/sqlc/cmd/sqlc
RUN go build -o sqlc-playground .

FROM heroku/heroku:20
WORKDIR /app
COPY --from=builder /go/bin/sqlc .
COPY --from=builder /go/src/app/sqlc-playground .
COPY --from=builder /go/src/app/static .
COPY --from=builder /go/src/app/index.tmpl.html  .
CMD ["sqlc-playground", "/app/go", "/app/sqlc"]
