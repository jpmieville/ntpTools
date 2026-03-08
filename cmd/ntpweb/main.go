package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"time"

	"ntptools/pkg/ntp"
)

const webTimeout = 5 * time.Second

//go:embed all:static
var staticFiles embed.FS

func main() {
	addr := flag.String("addr", ":8080", "Listen address (host:port)")
	flag.Parse()

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to create sub filesystem: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("/api/ntp", handleNTP)
	mux.HandleFunc("/api/servers", handleServers)

	fmt.Printf("NTP Tools web server listening on %s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func handleNTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	server := r.URL.Query().Get("server")
	if server == "" {
		server = ntp.DefaultServers[0]
	}

	versionStr := r.URL.Query().Get("version")
	version := uint8(4)
	if v, err := strconv.Atoi(versionStr); err == nil && (v == 3 || v == 4) {
		version = uint8(v)
	}

	client := ntp.NewClient(server, version, webTimeout)
	result, err := client.FetchTime()
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(result)
}

func handleServers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ntp.DefaultServers)
}
