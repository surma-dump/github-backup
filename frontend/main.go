package main

import (
	"flag"
	"log"
	"net/http"
)

var (
	listen = flag.String("listen", "localhost:8080", "Address to bind webserver to")
	static = flag.String("static", "static", "Path to static files")
	help   = flag.Bool("help", false, "Show this help")
)

func main() {
	flag.Parse()
	if *help {
		flag.PrintDefaults()
		return
	}

	http.Handle("/", http.FileServer(http.Dir(*static)))
	log.Printf("Starting webserver on %s...", *listen)
	if err := http.ListenAndServe(*listen, nil); err != nil {
		log.Fatalf("Error starting webserver: %s", err)
	}
}
