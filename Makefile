serve: sqlc-playground
		./sqlc-playground go ~/bin/sqlc-dev

sqlc-playground: $(wildcard *.go)
		go build .
