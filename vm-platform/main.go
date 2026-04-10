package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"vm-platform/handlers"
	"vm-platform/services"
)

//go:embed templates/*
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

func main() {
	// Initialize services
	platform := services.NewPlatformService()

	// Initialize handlers
	handler := handlers.NewHandler(platform)

	// Set up routes
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Serve static files
	staticContent, _ := fs.Sub(staticFS, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))

	// Serve the main dashboard
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, _ := templatesFS.ReadFile("templates/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	port := ":8080"
	fmt.Printf("╔══════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  VM Platform - Gestión de Máquinas Virtuales        ║\n")
	fmt.Printf("║  Servidor iniciado en http://localhost%s          ║\n", port)
	fmt.Printf("╚══════════════════════════════════════════════════════╝\n")

	log.Fatal(http.ListenAndServe(port, mux))
}
