package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dutchcoders/goftp"
	"github.com/garyburd/redigo/redis"
	"github.com/surma-dump/github-backup/common"
)

var (
	sshKey    = flag.String("key", "", "SSH key to use for cloning")
	ftpUrl    = flag.String("ftp", "", "FTP server to save backups to")
	redisUrl  = flag.String("redis", "", "Address of redis")
	frequency = flag.Duration("frequency", 24*time.Hour, "Frequency of backups")
	namespace = flag.String("namespace", "github-backup", "Database namespace")
	force     = flag.Bool("force", false, "Force download")
	help      = flag.Bool("help", false, "Show this help")
)

var (
	badCharacters = regexp.MustCompilePOSIX("[/@:!?*\\&]")
)

func main() {
	flag.Parse()
	if *help {
		flag.PrintDefaults()
		return
	}
	if *ftpUrl == "" || *redisUrl == "" {
		log.Fatalf("-ftp and -redis have to be set")
	}

	if *sshKey != "" {
		if err := addSshKey(*sshKey); err != nil {
			log.Fatalf("Could not add SSH key: %s", err)
		}
	}

	redisConn, err := common.ConnectRedis(*redisUrl)
	if err != nil {
		log.Fatalf("Could not connect to redis: %s", err)
	}
	defer redisConn.Close()

	ftpConn, ftpUrl, err := connectFtp(*ftpUrl)
	if err != nil {
		log.Fatalf("Could not connect to FTP server: %s", err)
	}
	defer ftpConn.Close()
	if err := ftpConn.Cwd(ftpUrl.Path); err != nil {
		log.Fatalf("Could not cd to target directory: %s", err)
	}

	for {
		if !*force {
			nextRun := lastRun(redisConn).Add(*frequency)
			if nextRun.After(time.Now()) {
				time.Sleep(nextRun.Sub(time.Now()))
				continue
			}
		}
		*force = false

		log.Printf("Downloading all the repos...")
		repos := repos(redisConn)
		for _, repo := range repos {
			log.Printf("Downloading %s...", repo)
			safeName := badCharacters.ReplaceAllString(repo, "_")
			r, err := downloadRepository(repo)
			if err != nil {
				log.Printf("Error downloading repository: %s", err)
				continue
			}

			if err := ftpConn.Stor(safeName+".tar.gz", r); err != nil {
				log.Printf("Error uploading: %s", err)
			}
		}
		log.Printf("Finished.")
		timestampLastRun(redisConn)
	}
}

func lastRun(conn redis.Conn) time.Time {
	ok, err := redis.Bool(conn.Do("EXISTS", *namespace+":lastrun"))
	if err != nil {
		log.Fatalf("Error querying database: %s", err)
	}
	if !ok {
		return time.Unix(0, 0)
	}

	ts, err := redis.String(conn.Do("GET", *namespace+":lastrun"))
	if err != nil {
		log.Fatalf("Error retrieving timestamp: %s", err)
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		log.Fatalf("Error parsing timestamp: %s", err)
	}
	return t
}

func timestampLastRun(conn redis.Conn) {
	_, err := conn.Do("SET", *namespace+":lastrun", time.Now().Format(time.RFC3339))
	if err != nil {
		log.Fatalf("Error writing timestamp: %s", err)
	}
}

func repos(conn redis.Conn) []string {
	repos, err := redis.Values(conn.Do("SMEMBERS", *namespace+":repos"))
	if err == redis.ErrNil {
		return []string{}
	}
	if err != nil {
		log.Fatalf("Error retrieving repo list: %s", err)
	}
	r := make([]string, 0, len(repos))
	if err := redis.ScanSlice(repos, &r); err != nil {
		log.Fatalf("Error parsing repo list: %s", err)
	}
	return r
}

func connectFtp(s string) (*goftp.FTP, *url.URL, error) {
	ftpUrl, err := url.Parse(s)
	if err != nil {
		log.Fatalf("Invalid ftp url: %s", err)
	}
	if ftpUrl.Scheme != "ftp" {
		log.Fatalf("Unsupported target scheme %s", ftpUrl.Scheme)
	}
	if !strings.Contains(ftpUrl.Host, ":") {
		ftpUrl.Host += ":21"
	}

	ftp, err := goftp.Connect(ftpUrl.Host)
	if err != nil {
		return ftp, ftpUrl, err
	}
	if ftpUrl.User == nil {
		return ftp, ftpUrl, err
	}
	user := ftpUrl.User.Username()
	pass, _ := ftpUrl.User.Password()
	return ftp, ftpUrl, ftp.Login(user, pass)
}

func downloadRepository(path string) (io.Reader, error) {
	repo := os.TempDir() + *namespace

	if err := os.MkdirAll(repo, os.FileMode(0700)); err != nil {
		return nil, err
	}

	cmd := exec.Command("git", "clone", "--bare", path)
	cmd.Dir = repo
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return tarDir(repo)
}

func tarDir(root string) (io.Reader, error) {
	r, w := io.Pipe()
	go func() {
		defer os.RemoveAll(root)
		defer w.Close()
		gzbuf := gzip.NewWriter(w)
		defer gzbuf.Close()
		defer gzbuf.Flush()
		archive := tar.NewWriter(gzbuf)
		defer archive.Close()
		defer archive.Flush()
		err := filepath.Walk(root, filepath.WalkFunc(func(path string, info os.FileInfo, err error) error {
			if path == root {
				return nil
			}
			relPath := strings.TrimPrefix(path, root)
			hdr := &tar.Header{
				Name:     strings.TrimPrefix(relPath, "/"),
				Mode:     int64(info.Mode() & os.ModePerm),
				Uid:      1000,
				Gid:      1000,
				Size:     info.Size(),
				Typeflag: tar.TypeReg,
			}
			if info.IsDir() {
				hdr.Typeflag = tar.TypeDir
				hdr.Size = 0
			}

			if err := archive.WriteHeader(hdr); err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			if _, err := io.Copy(archive, f); err != nil && err != io.EOF {
				return err
			}
			return nil
		}))
		if err != nil {
			log.Printf("Error creating archive: %s", err)
		}
	}()
	return r, nil
}

const (
	sshConfig = `
	IdentityFile /root/.ssh/github-backup
	StrictHostKeyChecking no
	`
)

func writeFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, os.FileMode(0700))
	if err != nil {
		return fmt.Errorf("Error creating file %s: %s", path, err)
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("Error writing file %s: %s", path, err)
	}
	return nil
}

func addSshKey(encKey string) error {
	key, err := base64.StdEncoding.DecodeString(encKey)
	if err != nil {
		return fmt.Errorf("Error decoding key: %s", err)
	}
	if err := os.MkdirAll("/root/.ssh", os.FileMode(0700)); err != nil {
		return fmt.Errorf("Error creating .ssh folder: %s", err)
	}

	if err := writeFile("/root/.ssh/github-backup", key); err != nil {
		return err
	}
	if err := writeFile("/root/.ssh/config", []byte(sshConfig)); err != nil {
		return err
	}
	return nil
}
