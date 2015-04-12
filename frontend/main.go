package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/garyburd/redigo/redis"
	gh "github.com/google/go-github/github"
	"github.com/surma-dump/github-backup/common"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

var (
	listen       = flag.String("listen", "localhost:8080", "Address to bind webserver to")
	clientId     = flag.String("id", "", "App ID of GitHub app")
	clientSecret = flag.String("secret", "", "Secret of GitHub app")
	publicUrl    = flag.String("public", "", "Public URL of the app")
	redisUrl     = flag.String("redis", "", "Address of redis")
	static       = flag.String("static", "static", "Path to static files")
	help         = flag.Bool("help", false, "Show this help")

	oauthConfig *oauth2.Config
	root        = context.Background()
)

type key int

const (
	RedisKey key = iota
	GithubApiKey
)

func main() {
	flag.Parse()
	if *help {
		flag.PrintDefaults()
		return
	}

	if *redisUrl == "" {
		log.Fatalf("-redis has to be set")
	}

	oauthConfig = &oauth2.Config{
		ClientID:     *clientId,
		ClientSecret: *clientSecret,
		RedirectURL:  *publicUrl + "/callback",
		Scopes:       []string{"repo"},
		Endpoint:     github.Endpoint,
	}

	redisConn, err := common.ConnectRedis(*redisUrl)
	if err != nil {
		log.Fatalf("Could not connect to redis: %s", err)
	}
	defer redisConn.Close()

	root = context.WithValue(root, RedisKey, redisConn)

	http.HandleFunc("/active", active)
	http.HandleFunc("/activate", activate)
	http.HandleFunc("/deactivate", deactivate)
	http.HandleFunc("/repos", listRepos)
	http.HandleFunc("/import", githubImport)
	http.HandleFunc("/callback", githubCallback)

	staticUrl, err := url.Parse(*static)
	if err != nil {
		log.Fatalf("Error parsing static parameter: %s", err)
	}
	if staticUrl.Scheme == "http" || staticUrl.Scheme == "https" {
		http.Handle("/", httputil.NewSingleHostReverseProxy(staticUrl))
	} else {
		http.Handle("/", http.FileServer(http.Dir(staticUrl.Path)))
	}

	log.Printf("Starting webserver on %s...", *listen)
	if err := http.ListenAndServe(*listen, nil); err != nil {
		log.Fatalf("Error starting webserver: %s", err)
	}
}

func active(w http.ResponseWriter, r *http.Request) {
	conn := root.Value(RedisKey).(redis.Conn)

	vals, err := redis.Values(conn.Do("SMEMBERS", "github-backup:repos"))
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
	conn := root.Value(RedisKey).(redis.Conn)
	name := r.FormValue("name")

	if name == "" {
		http.Error(w, "name query parameter missing", http.StatusInternalServerError)
		return
	}
	if _, err := conn.Do("SADD", "github-backup:repos", name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Error(w, "", http.StatusNoContent)
}

func deactivate(w http.ResponseWriter, r *http.Request) {
	conn := root.Value(RedisKey).(redis.Conn)
	name := r.FormValue("name")

	if name == "" {
		http.Error(w, "name query parameter missing", http.StatusInternalServerError)
		return
	}
	if _, err := conn.Do("SREM", "github-backup:repos", name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Error(w, "", http.StatusNoContent)
}

func listRepos(w http.ResponseWriter, r *http.Request) {
	conn := root.Value(RedisKey).(redis.Conn)

	vals, err := redis.Values(conn.Do("SMEMBERS", "github-backup:known_repos"))
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
	target := oauthConfig.AuthCodeURL("random", oauth2.ApprovalForce)
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

type GithubOptIn struct {
	http.RoundTripper
}

func (goi GithubOptIn) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Accept", "application/vnd.github.moondragon+json")
	return goi.RoundTripper.RoundTrip(r)
}

func githubCallback(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("state") != "random" {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	token, err := oauthConfig.Exchange(oauth2.NoContext, r.FormValue("code"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tokenSource := oauthConfig.TokenSource(oauth2.NoContext, token)
	t := &oauth2.Transport{Source: tokenSource}
	c := &http.Client{Transport: GithubOptIn{t}}
	ghApi := gh.NewClient(c)

	ctx := root
	ctx = context.WithValue(root, GithubApiKey, ghApi)
	go importRepos(ctx)
	fmt.Fprintf(w, "<script>window.close();</script>")
}

func importRepos(ctx context.Context) {
	ghApi := ctx.Value(GithubApiKey).(*gh.Client)
	conn := ctx.Value(RedisKey).(redis.Conn)

	currentPage := 1
	for currentPage != 0 {
		req, err := ghApi.NewRequest("GET", fmt.Sprintf("user/repos?page=%d", currentPage), nil)
		if err != nil {
			log.Printf("Error creating request: %s", err)
			return
		}
		repos := []gh.Repository{}
		resp, err := ghApi.Do(req, &repos)
		if err != nil {
			log.Printf("Error executing request: %s", err)
			return
		}
		for _, repo := range repos {
			if _, err := conn.Do("SADD", "github-backup:known_repos", *repo.SSHURL); err != nil {
				log.Printf("Error saving to database: %s", err)
			}
		}
		currentPage = resp.NextPage
	}
}
