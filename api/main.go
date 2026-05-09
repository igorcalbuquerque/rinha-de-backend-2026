package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func main() {
	runtime.GOMAXPROCS(1)

	resourcesPath := os.Getenv("RESOURCES_PATH")
	if resourcesPath == "" {
		resourcesPath = "/resources"
	}

	srv := &server{}
	srv.mccRisk = defaultMCCRisk()
	srv.norms = defaultNormalization()
	srv.ready = true

	mux := http.NewServeMux()
	mux.HandleFunc("/ready", srv.readyHandler)
	mux.HandleFunc("/fraud-score", srv.fraudScoreHandler)

	httpSrv := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Println("loading metadata...")
		mccRisk, norms, err := loadMetadata(resourcesPath)
		if err != nil {
			log.Printf("startup failed: %v", err)
			return
		}

		srv.mu.Lock()
		srv.mccRisk = mccRisk
		srv.norms = norms
		srv.ready = true
		srv.mu.Unlock()

		log.Println("ready: metadata loaded, loading references...")

		points, err := loadReferences(resourcesPath + "/references.json.gz")
		if err != nil {
			log.Printf("references failed: %v", err)
			return
		}

		srv.mu.Lock()
		srv.points = points
		srv.mu.Unlock()
		log.Printf("ready: %d reference vectors loaded, building VP-Tree...", len(points))

		tree := BuildVPTree(points)
		n := len(tree.points)

		srv.mu.Lock()
		srv.tree = tree
		srv.mu.Unlock()

		log.Printf("ready: %d reference vectors loaded", n)
	}()

	go func() {
		if err := serveHTTP(httpSrv); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpSrv.Shutdown(ctx)
}

func serveHTTP(httpSrv *http.Server) error {
	socketPath := os.Getenv("API_SOCKET")
	if socketPath == "" {
		return httpSrv.ListenAndServe()
	}

	if err := os.RemoveAll(socketPath); err != nil {
		return err
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(socketPath, 0o777); err != nil {
		ln.Close()
		return err
	}

	return httpSrv.Serve(ln)
}
