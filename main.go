package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gorilla/mux"
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

type Request struct {
	Query string `json:"query"`
}

type Response struct {
	Errored bool   `json:"errored"`
	Error   string `json:"error"`
	Sha     string `json:"sha"`
}

func trimPort(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
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

	cmd := exec.CommandContext(ctx, sqlcbin, "generate")
	cmd.Dir = dir
	stderr, err := cmd.CombinedOutput()
	if err != nil {
		return &Response{Errored: true, Error: string(stderr)}, nil
	}

	return &Response{Sha: sum}, nil
}

func main() {
	flag.Parse()

	gopath := flag.Arg(0)
	sqlcbin := flag.Arg(1)

	rpURL, _ := url.Parse("http://localhost:6061")
	proxy := httputil.NewSingleHostReverseProxy(rpURL)

	go func() {
		cmd := exec.CommandContext(context.Background(), "godoc", "-http=:6061")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(),
			"GOPATH="+gopath,
		)
		fmt.Println("Starting godoc on port :6061 with GOPATH=" + gopath)
		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}
	}()

	play := http.NewServeMux()
	fs := http.FileServer(http.Dir("static"))
	play.Handle("/static/", http.StripPrefix("/static", fs))
	play.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	play.HandleFunc("/generate", func(w http.ResponseWriter, r *http.Request) {
		// TODO: check body size
		// if err != nil {
		// 	http.Error(w, `{"error": "500"}`, http.StatusInternalServerError)
		// 	return
		// }
		defer r.Body.Close()
		resp, err := generate(r.Context(), filepath.Join(gopath, "src", "sqlc.dev"), sqlcbin, r.Body)
		if err != nil {
			fmt.Println(err)
			http.Error(w, `{"errored": true, "error": "500: Internal Server Error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.Encode(resp)
	})
	port := os.Getenv("PORT")
	if port == "" {
		port = "8086"
	}

	playHost := os.Getenv("PLAYGROUND_HOST")
	if playHost == "" {
		playHost = "playground.sqlc.test:" + port
	}
	docHost := os.Getenv("GODOC_HOST")
	if docHost == "" {
		docHost = "play-godoc.sqlc.test:" + port
	}

	w, err := os.Create("index.html")
	if err != nil {
		log.Fatalf("create index.html: %s", err)
	}

	tmpl := template.Must(template.ParseFiles("index.tmpl.html"))
	if err := tmpl.Execute(w, docHost); err != nil {
		log.Fatalf("template execution: %s", err)
	}

	r := mux.NewRouter()
	r.Host(trimPort(playHost)).Handler(play)
	r.Host(trimPort(docHost)).Handler(proxy)

	log.Fatal(http.ListenAndServe(":"+port, r))
}
