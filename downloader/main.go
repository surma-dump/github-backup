package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dutchcoders/goftp"
	"github.com/garyburd/redigo/redis"
)

var (
	sshKey    = flag.String("key", "", "SSH key to use for cloning")
	ftpUrl    = flag.String("ftp", "", "FTP server to save backups to")
	redisUrl  = flag.String("redis", "", "Address of redis")
	frequency = flag.Duration("frequency", 24*time.Hour, "Frequency of backups")
	help      = flag.Bool("help", false, "Show this help")
)

func main() {
	flag.Parse()
	if *help {
		flag.PrintDefaults()
		return
	}
	if *ftpUrl == "" || *sshKey == "" || *redisUrl == "" {
		log.Fatalf("-ftp, -key und -redis have to be set")
	}

	redisConn, err := connectRedis(*redisUrl)
	if err != nil {
		log.Fatalf("Could not connect to redis: %s", err)
	}
	defer redisConn.Close()

	ftpConn, err := connectFtp(*ftpUrl)
	if err != nil {
		log.Fatalf("Could not connect to FTP server: %s", err)
	}
	defer ftpConn.Close()
}

func connectRedis(s string) (redis.Conn, error) {
	redisUrl, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("Could not parse redis url: %s", err)
	}
	if redisUrl.Scheme != "redis" {
		return nil, fmt.Errorf("Unsupported redis scheme %s", redisUrl.Scheme)
	}

	return redis.Dial("tcp", redisUrl.Host)
}

func connectFtp(s string) (*goftp.FTP, error) {
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
		return ftp, err
	}
	if ftpUrl.User == nil {
		return ftp, err
	}
	user := ftpUrl.User.Username()
	pass, _ := ftpUrl.User.Password()
	return ftp, ftp.Login(user, pass)
}

func tmp() {
	buf, err := downloadRepository("surma/phrank")
	if err != nil {
		log.Fatalf("Could not download: %s", err)
	}

	ftp, err := goftp.Connect("10.0.0.2:21")
	if err != nil {
		log.Fatalf("Could not connect to ftp: %s", err)
	}
	defer ftp.Close()
	if err := ftp.Login("surma", ""); err != nil {
		log.Fatalf("Could not login: %s", err)
	}
	if err := ftp.Stor("/Scratch/tmp.tar.gz", buf); err != nil {
		log.Fatalf("Could not upload: %s", err)
	}

}

func downloadRepository(path string) (*bytes.Buffer, error) {
	repo := os.TempDir() + "github-backup"

	if err := os.MkdirAll(repo, os.FileMode(0700)); err != nil {
		return nil, err
	}
	defer os.RemoveAll(repo)

	cmd := exec.Command("git", "clone", "--bare", fmt.Sprintf("git@github.com:%s", path))
	cmd.Dir = repo
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return tarDir(repo)
}

func tarDir(root string) (*bytes.Buffer, error) {
	buf := &bytes.Buffer{}
	gzbuf := gzip.NewWriter(buf)
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
		return nil, err
	}
	return buf, nil
}
