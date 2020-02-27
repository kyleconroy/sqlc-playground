package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

const confJSON = `{
  "version": "1",
  "packages": [
    {
      "path": "db",
      "schema": "query.sql",
      "queries": "query.sql",
	  "emit_single_file": true
    }
  ]
}`

type Request struct {
	Query string `json:"query"`
}

type Response struct {
	Error  string `json:"error"`
	Output string `json:"output"`
	Sha    string `json:"sha"`
}

func generate(ctx context.Context, base string, rd io.Reader) (*Response, error) {
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
	out := filepath.Join(dir, "db", "db.go")

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

	sqlcbin := os.Getenv("SQLC_BINARY")
	if sqlcbin == "" {
		sqlcbin = "sqlc"
	}

	cmd := exec.CommandContext(ctx, sqlcbin, "generate")
	cmd.Dir = dir
	stderr, err := cmd.CombinedOutput()
	if err != nil {
		return &Response{Error: string(stderr)}, nil
	}

	dbgo, err := ioutil.ReadFile(out)
	if err != nil {
		return nil, err
	}

	return &Response{Output: string(dbgo), Sha: sum}, nil
}

func main() {
	flag.Parse()

	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static", fs))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	http.HandleFunc("/generate", func(w http.ResponseWriter, r *http.Request) {
		// TODO: check body size
		// if err != nil {
		// 	http.Error(w, `{"error": "500"}`, http.StatusInternalServerError)
		// 	return
		// }
		defer r.Body.Close()
		resp, err := generate(r.Context(), flag.Arg(0), r.Body)
		if err != nil {
			fmt.Println(err)
			http.Error(w, `{"error": "500"}`, http.StatusInternalServerError)
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
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
