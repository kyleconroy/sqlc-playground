module github.com/kyleconroy/sqlc-playground

// +heroku goVersion go1.13
// +heroku install ./... golang.org/x/tools/cmd/godoc

go 1.13

require (
	github.com/gorilla/mux v1.7.4
	golang.org/x/tools v0.0.0-20200227222343-706bc42d1f0d
)
