package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"vm-platform/models"
	"vm-platform/services"
)

// DeployHandler holds deploy-related HTTP handler dependencies
type DeployHandler struct {
	Deploy   *services.DeployService
	Platform *services.PlatformService
}

// NewDeployHandler creates a new deploy handler
func NewDeployHandler(deploy *services.DeployService, platform *services.PlatformService) *DeployHandler {
	return &DeployHandler{Deploy: deploy, Platform: platform}
}

// UploadAndDeploy handles file upload and deployment to VM
func (dh *DeployHandler) UploadAndDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	// Parse multipart form (max 100MB)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "Error al parsear formulario: "+err.Error())
		return
	}

	vmName := r.FormValue("vm_name")
	username := r.FormValue("username")
	destFolder := r.FormValue("dest_folder")
	execCommand := r.FormValue("exec_command")
	execParams := r.FormValue("exec_params")
	password := r.FormValue("password")

	if vmName == "" || username == "" || destFolder == "" || execCommand == "" {
		respondError(w, http.StatusBadRequest, "vm_name, username, dest_folder y exec_command son requeridos")
		return
	}

	// Get the uploaded file
	file, header, err := r.FormFile("zip_file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "Archivo zip es requerido: "+err.Error())
		return
	}
	defer file.Close()

	// Read file data
	zipData, err := io.ReadAll(file)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Error al leer archivo: "+err.Error())
		return
	}

	info, err := dh.Deploy.DeployApplication(vmName, username, destFolder, execCommand, execParams, password, zipData, header.Filename)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, fmt.Sprintf("Aplicación desplegada exitosamente en %s", destFolder), info)
}

// CreateService creates a systemd service for the deployed application
func (dh *DeployHandler) CreateService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req models.ServiceCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if req.VMName == "" || req.ServiceName == "" {
		respondError(w, http.StatusBadRequest, "vm_name y service_name son requeridos")
		return
	}

	if err := dh.Deploy.CreateSystemdService(req.VMName, req.ServiceName, req.RootPassword); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, fmt.Sprintf("Servicio '%s' creado exitosamente", req.ServiceName), nil)
}

// ServiceAction handles start, stop, restart, enable, disable actions
func (dh *DeployHandler) ServiceAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req struct {
		VMName       string `json:"vm_name"`
		Action       string `json:"action"`
		RootPassword string `json:"root_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if req.VMName == "" || req.Action == "" {
		respondError(w, http.StatusBadRequest, "vm_name y action son requeridos")
		return
	}

	if err := dh.Deploy.ServiceAction(req.VMName, req.Action, req.RootPassword); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	actionLabels := map[string]string{
		"start":   "iniciado",
		"stop":    "detenido",
		"restart": "reiniciado",
		"enable":  "habilitado al arranque",
		"disable": "deshabilitado del arranque",
	}

	respondSuccess(w, fmt.Sprintf("Servicio %s exitosamente", actionLabels[req.Action]), nil)
}

// GetServiceStatus returns the current service status
func (dh *DeployHandler) GetServiceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	vmName := r.URL.Query().Get("vm_name")
	if vmName == "" {
		respondError(w, http.StatusBadRequest, "vm_name es requerido")
		return
	}

	status, err := dh.Deploy.GetServiceStatus(vmName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, "Estado del servicio", status)
}

// GetLogContent returns the latest log content
func (dh *DeployHandler) GetLogContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	vmName := r.URL.Query().Get("vm_name")
	if vmName == "" {
		respondError(w, http.StatusBadRequest, "vm_name es requerido")
		return
	}

	lines := 50
	if l := r.URL.Query().Get("lines"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			lines = n
		}
	}

	content, err := dh.Deploy.GetLogContent(vmName, lines)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, "Contenido del log", content)
}

// StreamLogs provides Server-Sent Events for live log streaming
func (dh *DeployHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	vmName := r.URL.Query().Get("vm_name")
	if vmName == "" {
		http.Error(w, "vm_name es requerido", http.StatusBadRequest)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	outputChan := make(chan string, 100)
	stopChan := make(chan struct{})

	// Start streaming
	if err := dh.Deploy.StreamLog(vmName, outputChan, stopChan); err != nil {
		fmt.Fprintf(w, "data: [Error: %s]\n\n", err.Error())
		flusher.Flush()
		return
	}

	// Send initial message
	fmt.Fprintf(w, "data: [Conectado al stream de logs...]\n\n")
	flusher.Flush()

	// Stream until client disconnects
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			close(stopChan)
			return
		case line, ok := <-outputChan:
			if !ok {
				return
			}
			// Send each line as SSE event
			for _, l := range splitLines(line) {
				if l != "" {
					fmt.Fprintf(w, "data: %s\n\n", l)
				}
			}
			flusher.Flush()
		}
	}
}

// GetDeployments returns all deployments
func (dh *DeployHandler) GetDeployments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	deployments := dh.Deploy.GetAllDeployments()
	respondSuccess(w, "Deployments", deployments)
}

// ListDeployableVMs returns VMs that have a user created and are ready for deployment
func (dh *DeployHandler) ListDeployableVMs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	data := dh.Platform.GetDashboardData()
	type VMEntry struct {
		VMName   string `json:"vm_name"`
		Username string `json:"username"`
	}
	var vms []VMEntry

	// Include user VMs with users created
	for _, uvm := range data.UserVMs {
		if uvm.UserCreated {
			vms = append(vms, VMEntry{
				VMName:   uvm.Name,
				Username: uvm.Username,
			})
		}
	}

	// Also include base VMs with root keys (can deploy as root)
	for _, bvm := range data.BaseVMs {
		if bvm.KeysCreated {
			vms = append(vms, VMEntry{
				VMName:   bvm.Name,
				Username: "root",
			})
		}
	}

	if vms == nil {
		vms = []VMEntry{}
	}
	respondSuccess(w, "VMs disponibles para despliegue", vms)
}

// RegisterDeployRoutes registers all deploy-related HTTP routes
func (dh *DeployHandler) RegisterDeployRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/deploy/upload", dh.UploadAndDeploy)
	mux.HandleFunc("/api/deploy/list", dh.GetDeployments)
	mux.HandleFunc("/api/deploy/vms", dh.ListDeployableVMs)
	mux.HandleFunc("/api/service/create", dh.CreateService)
	mux.HandleFunc("/api/service/action", dh.ServiceAction)
	mux.HandleFunc("/api/service/status", dh.GetServiceStatus)
	mux.HandleFunc("/api/service/logs", dh.GetLogContent)
	mux.HandleFunc("/api/service/stream", dh.StreamLogs)
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range split(s) {
		lines = append(lines, line)
	}
	return lines
}

func split(s string) []string {
	var result []string
	current := ""
	for _, ch := range s {
		if ch == '\n' {
			result = append(result, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
