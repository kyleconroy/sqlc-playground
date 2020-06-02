package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gorilla/mux"
	"goji.io"
	"goji.io/pat"
)

const confJSON = `{
  "version": "1",
  "packages": [
    {
      "path": "db",
      "schema": "query.sql",
      "queries": "query.sql"
    }
  ]
}`

var tmpl *template.Template

type Request struct {
	Query string `json:"query"`
}

type File struct {
	Name     string `json:"name"`
	Contents string `json:"contents"`
}

type Response struct {
	Errored bool   `json:"errored"`
	Error   string `json:"error"`
	Sha     string `json:"sha"`
	Files   []File `json:"files"`
}

func trimPort(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
}

func buildResponse(dir string) (*Response, error) {
	elog := filepath.Join(dir, "out.log")
	if _, err := os.Stat(elog); err == nil {
		blob, err := ioutil.ReadFile(elog)
		if err != nil {
			return nil, err
		}
		if len(blob) > 0 {
			return &Response{Errored: true, Error: string(blob)}, nil
		}
	}
	resp := Response{}
	files, err := ioutil.ReadDir(filepath.Join(dir, "db"))
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		contents, err := ioutil.ReadFile(filepath.Join(dir, "db", file.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.Name(), err)
		}
		resp.Files = append(resp.Files, File{
			Name:     filepath.Join("db", file.Name()),
			Contents: string(contents),
		})
	}
	return &resp, nil
}

func generate(ctx context.Context, base, sqlcbin string, rd io.Reader) (*Response, error) {
	blob, err := ioutil.ReadAll(rd)
	if err != nil {
		return nil, err
	}

	var req Request
	if err := json.Unmarshal(blob, &req); err != nil {
		return nil, err
	}

	sum := fmt.Sprintf("%x", sha256.Sum256(blob))
	dir := filepath.Join(base, sum)
	conf := filepath.Join(dir, "sqlc.json")
	query := filepath.Join(dir, "query.sql")
	elog := filepath.Join(dir, "out.log")

	// Create the directory
	os.MkdirAll(dir, 0777)

	// Write the configuration file
	if err := ioutil.WriteFile(conf, []byte(confJSON), 0644); err != nil {
		return nil, err
	}

	// Write the SQL
	if err := ioutil.WriteFile(query, []byte(req.Query), 0644); err != nil {
		return nil, err
	}

	// Create log
	f, err := os.Create(elog)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	mwriter := io.MultiWriter(f, &buf)
	cmd := exec.CommandContext(ctx, sqlcbin, "generate")
	cmd.Stderr = mwriter
	cmd.Stdout = mwriter
	cmd.Dir = dir
	cmd.Run()

	resp, err := buildResponse(dir)
	if err != nil {
		return nil, err
	}
	resp.Sha = sum
	return resp, nil
}

type tmplCtx struct {
	DocHost  string
	SQL      string
	Response template.JS
	Stderr   string
	Pkg      string
}

func handlePlay(ctx context.Context, w http.ResponseWriter, gopath, pkgPath string) {
	dir := filepath.Join(gopath, "src", "sqlc.dev", pkgPath)
	filename := filepath.Join(dir, "query.sql")
	blob, err := ioutil.ReadFile(filename)
	if err != nil {
		http.Error(w, "Internal server error: ReadFile", http.StatusInternalServerError)
		return
	}

	resp, err := buildResponse(dir)
	if err != nil {
		http.Error(w, "Internal server error: buildResponse", http.StatusInternalServerError)
		return
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "Internal server error: Marshal", http.StatusInternalServerError)
	}

	var out bytes.Buffer
	json.HTMLEscape(&out, payload)

	tctx := tmplCtx{
		SQL:      string(blob),
		Pkg:      pkgPath,
		Response: template.JS(out.String()),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, tctx); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func main() {
	flag.Parse()

	gopath := flag.Arg(0)
	sqlcbin := flag.Arg(1)

	if gopath == "" {
		log.Fatalf("arg: gopath is empty")
	}
	if sqlcbin == "" {
		log.Fatalf("arg: sqlcbin is empty")
	}

	tmpl = template.Must(template.ParseFiles("index.tmpl.html"))
	port := os.Getenv("PORT")
	if port == "" {
		port = "8086"
	}
	playHost := os.Getenv("PLAYGROUND_HOST")
	if playHost == "" {
		playHost = "playground.sqlc.test:" + port
	}

	play := goji.NewMux()
	play.HandleFunc(pat.Get("/p/:checksum"), func(w http.ResponseWriter, r *http.Request) {
		path := pat.Param(r, "checksum")
		sha, err := hex.DecodeString(path)
		if err != nil {
			http.Error(w, "Invalid SHA: hex decode failed", http.StatusBadRequest)
			return
		}
		if len(sha) != 32 {
			http.Error(w, fmt.Sprintf("Invalid SHA: length %d", len(sha)), http.StatusBadRequest)
			return
		}
		handlePlay(r.Context(), w, gopath, filepath.Join("p", path))
	})

	play.HandleFunc(pat.Get("/docs/:section"), func(w http.ResponseWriter, r *http.Request) {
		handlePlay(r.Context(), w, gopath, filepath.Join("docs", pat.Param(r, "section")))
	})

	play.HandleFunc(pat.Get("/"), func(w http.ResponseWriter, r *http.Request) {
		handlePlay(r.Context(), w, gopath, filepath.Join("docs", "authors"))
	})

	play.HandleFunc(pat.Post("/generate"), func(w http.ResponseWriter, r *http.Request) {
		// TODO: check body size
		// if err != nil {
		// 	http.Error(w, `{"error": "500"}`, http.StatusInternalServerError)
		// 	return
		// }
		defer r.Body.Close()
		resp, err := generate(r.Context(), filepath.Join(gopath, "src", "sqlc.dev", "p"), sqlcbin, r.Body)
		if err != nil {
			fmt.Println("error", err)
			http.Error(w, `{"errored": true, "error": "500: Internal Server Error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.Encode(resp)
	})

	fs := http.FileServer(http.Dir(filepath.Join("static")))
	srv := http.NewServeMux()
	srv.Handle("/static/", http.StripPrefix("/static", fs))
	srv.Handle("/", play)

	r := mux.NewRouter()
	r.Host(trimPort(playHost)).Handler(srv)

	log.Printf("starting on :%s...\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
