package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dutchcoders/goftp"
)

func main() {
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
