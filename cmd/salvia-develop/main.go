package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	logger := log.New(os.Stdout, "[salvia-develop] ", log.LstdFlags)

	apiAddr := envOr("SALVIA_DEVELOP_API_ADDR", "127.0.0.1:3000")
	viteHost := envOr("SALVIA_DEVELOP_VITE_HOST", "127.0.0.1")
	vitePort := envOr("SALVIA_DEVELOP_VITE_PORT", "5173")
	salviaDir := envOr("SALVIA_DEVELOP_DIR", "salvia")

	dir, err := filepath.Abs(salviaDir)
	if err != nil {
		logger.Fatalf("resolve Salvia directory: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		logger.Fatalf("Salvia directory not found: %s", dir)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", apiAddr)
	if err != nil {
		logger.Fatalf("start dummy API on %s: %v (set SALVIA_DEVELOP_API_ADDR to change the address)", apiAddr, err)
	}
	server := &http.Server{
		Handler:           newDummyAPI(buildDemoData()),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	logger.Printf("dummy API listening on http://%s", listener.Addr().String())

	proxyTarget := "http://" + listener.Addr().String()
	vite, err := viteCommand(dir, viteHost, vitePort, proxyTarget)
	if err != nil {
		_ = server.Close()
		logger.Fatalf("%v", err)
	}

	if err := vite.Start(); err != nil {
		_ = server.Close()
		logger.Fatalf("start Vite: %v", err)
	}
	logger.Printf("Vite dev server starting at http://%s:%s", viteHost, vitePort)
	logger.Printf("proxying /api -> %s", proxyTarget)
	logger.Printf("Salvia is already authenticated as %s (%s); dummy data is immutable", demoUsername, demoAccountID)

	viteErr := make(chan error, 1)
	go func() { viteErr <- vite.Wait() }()

	select {
	case <-ctx.Done():
		logger.Printf("shutting down")
		if vite.Process != nil {
			_ = vite.Process.Signal(syscall.SIGINT)
		}
		select {
		case <-viteErr:
		case <-time.After(5 * time.Second):
			if vite.Process != nil {
				_ = vite.Process.Kill()
			}
			<-viteErr
		}
	case err := <-viteErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Printf("Vite exited: %v", err)
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Printf("shut down dummy API: %v", err)
	}
	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("dummy API stopped: %v", err)
			os.Exit(1)
		}
	default:
	}
}

func viteCommand(dir, host, port, proxyTarget string) (*exec.Cmd, error) {
	args := []string{"--host", host, "--port", port, "--strictPort"}
	env := append(os.Environ(), "VITE_API_PROXY_TARGET="+proxyTarget)
	// Prefer the installed binary so arguments are forwarded verbatim.
	if viteBin := filepath.Join(dir, "node_modules", ".bin", "vite"); fileExists(viteBin) {
		cmd := exec.Command(viteBin, args...)
		cmd.Dir = dir
		cmd.Env = env
		return cmd, nil
	}
	if pnpm, err := exec.LookPath("pnpm"); err == nil {
		cmd := exec.Command(pnpm, append([]string{"exec", "vite"}, args...)...)
		cmd.Dir = dir
		cmd.Env = env
		return cmd, nil
	}
	if npx, err := exec.LookPath("npx"); err == nil {
		cmd := exec.Command(npx, append([]string{"--no-install", "vite"}, args...)...)
		cmd.Dir = dir
		cmd.Env = env
		return cmd, nil
	}
	return nil, fmt.Errorf("vite not found; run `pnpm install` in %s", dir)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
