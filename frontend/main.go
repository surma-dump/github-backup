package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"

	"github.com/garyburd/redigo/redis"
	gh "github.com/google/go-github/github"
	"github.com/surma-dump/github-backup/common"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

var (
	listen       = flag.String("listen", "localhost:8080", "Address to bind webserver to")
	clientID     = flag.String("id", "", "App ID of GitHub app")
	clientSecret = flag.String("secret", "", "Secret of GitHub app")
	publicURL    = flag.String("public", "", "Public URL of the app")
	redisURL     = flag.String("redis", "", "Address of redis")
	static       = flag.String("static", "static", "Path to static files")
	namespace    = flag.String("namespace", "github-backup", "Database namespace")
	help         = flag.Bool("help", false, "Show this help")

	oauthConfig *oauth2.Config
	root        = context.Background()
)

type key int

const (
	redisKey key = iota
	githubAPIKey
	importUserRepoKey
	importStarredRepoKey
)

func main() {
	flag.Parse()
	if *help {
		flag.PrintDefaults()
		return
	}

	if *redisURL == "" {
		log.Fatalf("-redis has to be set")
	}

	oauthConfig = &oauth2.Config{
		ClientID:     *clientID,
		ClientSecret: *clientSecret,
		RedirectURL:  *publicURL + "/callback",
		Scopes:       []string{"repo"},
		Endpoint:     github.Endpoint,
	}

	if err := common.CheckRedis(*redisURL); err != nil {
		log.Fatalf("Could not connect to redis: %s", err)
	}
	pool := common.CreateRedisPool(*redisURL)
	defer pool.Close()
	root = context.WithValue(root, redisKey, pool)

	http.HandleFunc("/active", active)
	http.HandleFunc("/activate", activate)
	http.HandleFunc("/deactivate", deactivate)
	http.HandleFunc("/repos", listRepos)
	http.HandleFunc("/import", githubImport)
	http.HandleFunc("/callback", githubCallback)

	staticURL, err := url.Parse(*static)
	if err != nil {
		log.Fatalf("Error parsing static parameter: %s", err)
	}
	if staticURL.Scheme == "http" || staticURL.Scheme == "https" {
		http.Handle("/", httputil.NewSingleHostReverseProxy(staticURL))
	} else {
		http.Handle("/", http.FileServer(http.Dir(staticURL.Path)))
	}

	log.Printf("Starting webserver on %s...", *listen)
	if err := http.ListenAndServe(*listen, nil); err != nil {
		log.Fatalf("Error starting webserver: %s", err)
	}
}

func active(w http.ResponseWriter, r *http.Request) {
	pool := root.Value(redisKey).(*redis.Pool)
	conn := pool.Get()
	defer conn.Close()

	vals, err := redis.Values(conn.Do("SMEMBERS", *namespace+":repos"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	repos := []string{}
	if err := redis.ScanSlice(vals, &repos); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repos)
}

func activate(w http.ResponseWriter, r *http.Request) {
	pool := root.Value(redisKey).(*redis.Pool)
	conn := pool.Get()
	defer conn.Close()
	name := r.FormValue("name")

	if name == "" {
		http.Error(w, "name query parameter missing", http.StatusInternalServerError)
		return
	}
	if _, err := conn.Do("SADD", *namespace+":repos", name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Error(w, "", http.StatusNoContent)
}

func deactivate(w http.ResponseWriter, r *http.Request) {
	conn := root.Value(redisKey).(redis.Conn)
	name := r.FormValue("name")

	if name == "" {
		http.Error(w, "name query parameter missing", http.StatusInternalServerError)
		return
	}
	if _, err := conn.Do("SREM", *namespace+":repos", name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Error(w, "", http.StatusNoContent)
}

func listRepos(w http.ResponseWriter, r *http.Request) {
	pool := root.Value(redisKey).(*redis.Pool)
	conn := pool.Get()
	defer conn.Close()

	vals, err := redis.Values(conn.Do("SMEMBERS", *namespace+":known_repos"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	repos := []string{}
	if err := redis.ScanSlice(vals, &repos); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repos)
}

func githubImport(w http.ResponseWriter, r *http.Request) {
	target := oauthConfig.AuthCodeURL(r.URL.RawQuery, oauth2.ApprovalForce)
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

type githubOptIn struct {
	http.RoundTripper
}

func (goi githubOptIn) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Accept", "application/vnd.github.moondragon+json")
	return goi.RoundTripper.RoundTrip(r)
}

func githubCallback(w http.ResponseWriter, r *http.Request) {
	ctx := root

	state, err := url.ParseQuery(r.FormValue("state"))
	if err != nil {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}
	ctx = context.WithValue(ctx, importUserRepoKey, false)
	if state.Get("user") == "true" {
		ctx = context.WithValue(ctx, importUserRepoKey, true)
	}
	ctx = context.WithValue(ctx, importStarredRepoKey, false)
	if state.Get("starred") == "true" {
		ctx = context.WithValue(ctx, importStarredRepoKey, true)
	}

	token, err := oauthConfig.Exchange(oauth2.NoContext, r.FormValue("code"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tokenSource := oauthConfig.TokenSource(oauth2.NoContext, token)
	t := &oauth2.Transport{Source: tokenSource}
	c := &http.Client{Transport: githubOptIn{t}}
	ghAPI := gh.NewClient(c)

	ctx = context.WithValue(ctx, githubAPIKey, ghAPI)
	go importRepos(ctx)
	fmt.Fprintf(w, "<script>window.close();</script>")
}

func importRepos(ctx context.Context) {
	ghAPI := ctx.Value(githubAPIKey).(*gh.Client)
	pool := root.Value(redisKey).(*redis.Pool)
	conn := pool.Get()
	defer conn.Close()

	ch := make(chan gh.Repository)
	wg := &sync.WaitGroup{}
	if ctx.Value(importUserRepoKey).(bool) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			paginatedRepos(ch, ghAPI, "/user/repos")
		}()
	}
	if ctx.Value(importStarredRepoKey).(bool) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			paginatedRepos(ch, ghAPI, "/user/starred")
		}()
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for repo := range ch {
		if _, err := conn.Do("SADD", *namespace+":known_repos", *repo.SSHURL); err != nil {
			log.Printf("Error saving to database: %s", err)
		}
	}
}

func paginatedRepos(ch chan gh.Repository, ghAPI *gh.Client, url string) {
	currentPage := 1
	for currentPage != 0 {
		req, err := ghAPI.NewRequest("GET", fmt.Sprintf(url+"?page=%d", currentPage), nil)
		if err != nil {
			log.Printf("Error creating request: %s", err)
			return
		}
		repos := []gh.Repository{}
		resp, err := ghAPI.Do(req, &repos)
		if err != nil {
			log.Printf("Error executing request: %s", err)
			return
		}
		for _, repo := range repos {
			ch <- repo
		}
		currentPage = resp.NextPage
	}
}
