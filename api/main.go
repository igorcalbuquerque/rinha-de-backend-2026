package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	resourcesPath := os.Getenv("RESOURCES_PATH")
	if resourcesPath == "" {
		resourcesPath = "/resources"
	}

	srv := &server{}

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

		log.Println("ready: metadata loaded, building VP-Tree...")

		points, err := loadReferences(resourcesPath + "/references.json.gz")
		if err != nil {
			log.Printf("references failed: %v", err)
			return
		}

		tree := BuildVPTree(points)
		srv.mu.Lock()
		srv.tree = tree
		srv.mu.Unlock()
		log.Printf("ready: %d reference vectors loaded", len(tree.points))
	}()

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
