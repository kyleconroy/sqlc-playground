package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	"strings"

	"cloud.google.com/go/storage"
	"github.com/gorilla/mux"
	"goji.io"
	"goji.io/pat"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	_ "github.com/kyleconroy/sqlc"
)

const confJSON = `{
  "version": "1",
  "packages": [
    {
      "path": "db",
      "engine": "postgresql",
      "schema": "query.sql",
      "queries": "query.sql"
    }
  ]
}`

var tmpl *template.Template
var bucket *storage.BucketHandle

type Request struct {
	Query  string `json:"query"`
	Config string `json:"config"`
}

type File struct {
	Name        string `json:"name"`
	Contents    string `json:"contents"`
	ContentType string `json:"contentType"`
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

func buildOutput(dir string) (*Response, error) {
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
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		contents, err := ioutil.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", info.Name(), err)
		}
		resp.Files = append(resp.Files, File{
			Name:        strings.TrimPrefix(path, dir+"/"),
			Contents:    string(contents),
			ContentType: "text/x-go",
		})
		return nil
	})
	return &resp, err
}

func buildInput(dir string) (*Response, error) {
	files := []string{"query.sql", "sqlc.json"}
	resp := Response{}
	for _, file := range files {
		if _, err := os.Stat(filepath.Join(dir, file)); os.IsNotExist(err) {
			continue
		}
		contents, err := ioutil.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		resp.Files = append(resp.Files, File{
			Name:        file,
			Contents:    string(contents),
			ContentType: fmt.Sprintf("text/x-%s", strings.ReplaceAll(filepath.Ext(file), ".", "")),
		})
	}
	return &resp, nil
}

func save(base, dir string) {
	if bucket == nil {
		return
	}
	ctx := context.Background()

	walker := func(path string, info os.FileInfo, err error) error {
		fmt.Println(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}

		obj := bucket.Object(rel)
		w := obj.NewWriter(ctx)

		_, err = io.Copy(w, file)
		if err != nil {
			return err
		}

		if err := w.Close(); err != nil {
			return err
		}

		return nil
	}

	err := filepath.Walk(dir, walker)
	if err != nil {
		log.Printf("save err: %s\n", err)
	}
}

func generate(ctx context.Context, gopath, base, sqlcbin string, rd io.Reader) (*Response, error) {
	blob, err := ioutil.ReadAll(rd)
	if err != nil {
		return nil, err
	}

	var req Request
	if err := json.Unmarshal(blob, &req); err != nil {
		return nil, err
	}

	cfg := req.Config
	if cfg == "" {
		cfg = confJSON
	}

	if req.Query == "" {
		return nil, fmt.Errorf("empty query")
	}

	h := sha256.New()
	h.Write([]byte("sqlc-1.6.0"))
	h.Write([]byte(cfg))
	h.Write([]byte(req.Query))
	sum := fmt.Sprintf("%x", h.Sum(nil))
	dir := filepath.Join(base, sum)
	conf := filepath.Join(dir, "sqlc.json")
	query := filepath.Join(dir, "query.sql")
	elog := filepath.Join(dir, "out.log")

	// Create the directory
	os.MkdirAll(dir, 0777)

	// Write the configuration file
	if err := ioutil.WriteFile(conf, []byte(cfg), 0644); err != nil {
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

	go save(gopath, dir)

	resp, err := buildOutput(dir)
	if err != nil {
		return nil, err
	}
	resp.Sha = sum
	return resp, nil
}

type tmplCtx struct {
	DocHost string
	Input   template.JS
	Output  template.JS
	Stderr  string
	Pkg     string
}

func sync(ctx context.Context, gopath, pkgPath string) error {
	dir := filepath.Join(gopath, "src", "sqlc.dev", pkgPath)
	if !strings.HasPrefix(pkgPath, "p") {
		return nil
	}
	if _, err := os.Stat(dir); err == nil {
		return nil
	}

	fmt.Printf("%s doesn't exist, syning...\n", dir)

	query := &storage.Query{Prefix: filepath.Join("src", "sqlc.dev", pkgPath)}
	it := bucket.Objects(ctx, query)
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		obj := bucket.Object(attrs.Name)
		r, err := obj.NewReader(ctx)
		if err != nil {
			return err
		}
		defer r.Close()

		folder := filepath.Dir(filepath.Join(gopath, attrs.Name))
		os.MkdirAll(folder, 0755)

		f, err := os.Create(filepath.Join(gopath, attrs.Name))
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(f, r); err != nil {
			return err
		}
	}
	return nil
}

func handlePlay(ctx context.Context, w http.ResponseWriter, gopath, pkgPath string) {
	dir := filepath.Join(gopath, "src", "sqlc.dev", pkgPath)

	if err := sync(ctx, gopath, pkgPath); err != nil {
		log.Printf("sync error: %s\n", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	var input, output template.JS
	{
		resp, err := buildInput(dir)
		if err != nil {
			log.Println("buildInput", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		payload, err := json.Marshal(resp)
		if err != nil {
			log.Println("buildInput/marshal", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		var out bytes.Buffer
		json.HTMLEscape(&out, payload)
		input = template.JS(out.String())
	}

	{
		resp, err := buildOutput(dir)
		if err != nil {
			log.Println("buildOutput", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		payload, err := json.Marshal(resp)
		if err != nil {
			log.Println("buildOutput/marshal", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		var out bytes.Buffer
		json.HTMLEscape(&out, payload)
		output = template.JS(out.String())
	}

	tctx := tmplCtx{
		Pkg:    pkgPath,
		Input:  input,
		Output: output,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, tctx); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func main() {
	var err error
	ctx := context.Background()

	flag.Parse()
	gopath := flag.Arg(0)
	sqlcbin := flag.Arg(1)
	bucketName := os.Getenv("CLOUD_BUCKET_NAME")
	bucketAuth := os.Getenv("CLOUD_BUCKET_AUTH")

	if gopath == "" {
		log.Fatalf("arg: gopath is empty")
	}
	if sqlcbin == "" {
		log.Fatalf("arg: sqlcbin is empty")
	}
	if bucketName == "" {
		log.Fatalf("env: CLOUD_BUCKET_NAME is empty")
	}
	if bucketAuth == "" {
		log.Fatalf("env: CLOUD_BUCKET_AUTH is empty")
	}

	jsonCreds, err := base64.StdEncoding.DecodeString(bucketAuth)
	if err != nil {
		log.Fatal(err)
	}

	creds, err := google.CredentialsFromJSON(ctx, jsonCreds, storage.ScopeReadWrite)
	if err != nil {
		log.Fatal(err)
	}

	client, err := storage.NewClient(ctx, option.WithCredentials(creds)) // File("/Users/kyle/Downloads/sqlc-playground-32bcae44d539.json"))
	if err != nil {
		log.Fatalf("storage client: %s", err)
	}
	bucket = client.Bucket(bucketName)

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
		resp, err := generate(r.Context(), gopath, filepath.Join(gopath, "src", "sqlc.dev", "p"), sqlcbin, r.Body)
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
