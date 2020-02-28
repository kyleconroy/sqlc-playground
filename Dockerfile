FROM golang:1.13

COPY . /workspace/app
WORKDIR /workspace/app

RUN go get golang.org/x/tools/cmd/godoc
RUN go install ./...

CMD sqlc-playground
