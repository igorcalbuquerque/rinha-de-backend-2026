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
		log.Println("loading classifier...")
		classifier, mccRisk, norms, err := loadAll(resourcesPath)
		if err != nil {
			log.Printf("startup failed: %v", err)
			return
		}

		srv.mu.Lock()
		srv.classifier = classifier
		srv.mccRisk = mccRisk
		srv.norms = norms
		srv.ready = true
		srv.mu.Unlock()

		log.Printf("ready: %d classifier buckets loaded", len(classifier.specific))
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
