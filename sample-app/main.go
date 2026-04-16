package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	// Determine the directory where the executable is located
	execPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error al obtener ruta del ejecutable: %v\n", err)
		os.Exit(1)
	}
	execDir := filepath.Dir(execPath)

	// Log file in the same directory as the executable
	logFilePath := filepath.Join(execDir, "app_log.txt")

	// Determine starting counter from existing file
	counter := getLastCounter(logFilePath)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("Aplicación iniciada. Escribiendo en: %s (contador inicial: %d)\n", logFilePath, counter+1)

	// Default interval: 5 seconds
	interval := 5 * time.Second

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Write first line immediately
	counter++
	writeLine(logFilePath, counter)

	for {
		select {
		case <-ticker.C:
			counter++
			writeLine(logFilePath, counter)
		case sig := <-sigChan:
			fmt.Printf("\nSeñal recibida: %v. Cerrando aplicación.\n", sig)
			return
		}
	}
}

func writeLine(filePath string, counter int) {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error al abrir archivo: %v\n", err)
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%d - %s\n", counter, timestamp)
	f.WriteString(line)
	fmt.Print(line)
}

func getLastCounter(filePath string) int {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0
	}

	lines := splitLines(string(data))
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line == "" {
			continue
		}
		var num int
		_, err := fmt.Sscanf(line, "%d -", &num)
		if err == nil {
			return num
		}
	}
	return 0
}

func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, ch := range s {
		if ch == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
