package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"vm-platform/models"
	"vm-platform/services"
)

// Handler holds all HTTP handler dependencies
type Handler struct {
	Platform *services.PlatformService
}

// NewHandler creates a new handler
func NewHandler(platform *services.PlatformService) *Handler {
	return &Handler{Platform: platform}
}

// respondJSON sends a JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError sends a JSON error response
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, models.APIResponse{
		Success: false,
		Message: message,
	})
}

// respondSuccess sends a JSON success response
func respondSuccess(w http.ResponseWriter, message string, data interface{}) {
	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// GetDashboard returns dashboard data
func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	data := h.Platform.GetDashboardData()
	respondSuccess(w, "Dashboard data", data)
}

// ListAvailableVMs lists all VMs registered in VirtualBox
func (h *Handler) ListAvailableVMs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	vms, err := h.Platform.VBox.ListVMs()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("Error al listar VMs: %v", err))
		return
	}
	respondSuccess(w, "Lista de VMs", vms)
}

// AddBaseVM registers a base VM
func (h *Handler) AddBaseVM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req models.CreateBaseVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "El nombre de la máquina virtual es requerido")
		return
	}

	vm, err := h.Platform.AddBaseVM(req.Name, req.Description)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondSuccess(w, fmt.Sprintf("Máquina virtual base '%s' agregada exitosamente", req.Name), vm)
}

// CreateRootKeys creates and deploys root SSH keys
func (h *Handler) CreateRootKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req struct {
		VMName       string `json:"vm_name"`
		RootPassword string `json:"root_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if req.VMName == "" || req.RootPassword == "" {
		respondError(w, http.StatusBadRequest, "Nombre de VM y contraseña de root son requeridos")
		return
	}

	if err := h.Platform.CreateRootKeys(req.VMName, req.RootPassword); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, "Llaves de root creadas y desplegadas exitosamente", nil)
}

// DownloadRootKeys serves the root private key for download
func (h *Handler) DownloadRootKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	vmName := r.URL.Query().Get("vm_name")
	if vmName == "" {
		respondError(w, http.StatusBadRequest, "Nombre de VM requerido")
		return
	}

	keyPath, err := h.Platform.GetRootKeyPath(vmName)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=id_rsa_root_%s", vmName))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, keyPath)
}

// CreateDisk creates a multi-attach disk
func (h *Handler) CreateDisk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req struct {
		BaseVMName string `json:"base_vm_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	disk, err := h.Platform.CreateMultiAttachDisk(req.BaseVMName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, "Disco multiconexión creado exitosamente", disk)
}

// DisconnectDisk disconnects a disk
func (h *Handler) DisconnectDisk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req struct {
		DiskName string `json:"disk_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if err := h.Platform.DisconnectDisk(req.DiskName); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, "Disco desconectado exitosamente", nil)
}

// ConnectDisk connects a disk
func (h *Handler) ConnectDisk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req struct {
		DiskName string `json:"disk_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if err := h.Platform.ConnectDisk(req.DiskName); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, "Disco conectado exitosamente", nil)
}

// DeleteDisk removes a disk
func (h *Handler) DeleteDisk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req struct {
		DiskName string `json:"disk_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if err := h.Platform.DeleteDisk(req.DiskName); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, "Disco eliminado exitosamente", nil)
}

// CreateUserVM creates a user VM
func (h *Handler) CreateUserVM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req models.CreateUserVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if req.Name == "" || req.DiskName == "" {
		respondError(w, http.StatusBadRequest, "Nombre de VM y disco son requeridos")
		return
	}

	vm, err := h.Platform.CreateUserVM(req.Name, req.Description, req.DiskName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, fmt.Sprintf("Máquina virtual '%s' creada exitosamente", req.Name), vm)
}

// CreateVMUser creates a user in a VM
func (h *Handler) CreateVMUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req models.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if req.VMName == "" || req.Username == "" || req.Password == "" || req.RootPassword == "" {
		respondError(w, http.StatusBadRequest, "Nombre de VM, usuario, contraseña y contraseña root son requeridos")
		return
	}

	if err := h.Platform.CreateVMUser(req.VMName, req.Username, req.Password, req.RootPassword); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, fmt.Sprintf("Usuario '%s' creado exitosamente en '%s'", req.Username, req.VMName), nil)
}

// DownloadUserKeys serves user private key for download
func (h *Handler) DownloadUserKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	vmName := r.URL.Query().Get("vm_name")
	if vmName == "" {
		respondError(w, http.StatusBadRequest, "Nombre de VM requerido")
		return
	}

	keyPath, err := h.Platform.GetUserKeyPath(vmName)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(keyPath)))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, keyPath)
}

// ListVMUsers returns all user VMs that have a user created
func (h *Handler) ListVMUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	data := h.Platform.GetDashboardData()
	type UserEntry struct {
		VMName     string `json:"vm_name"`
		Username   string `json:"username"`
		BaseVMName string `json:"base_vm_name"`
		DiskName   string `json:"disk_name"`
	}
	var users []UserEntry
	for _, uvm := range data.UserVMs {
		if uvm.UserCreated {
			users = append(users, UserEntry{
				VMName:     uvm.Name,
				Username:   uvm.Username,
				BaseVMName: uvm.BaseVMName,
				DiskName:   uvm.DiskName,
			})
		}
	}
	if users == nil {
		users = []UserEntry{}
	}
	respondSuccess(w, "Lista de usuarios", users)
}

// RepairKeys regenerates and redeploys both root and user SSH keys using root password
func (h *Handler) RepairKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req struct {
		VMName       string `json:"vm_name"`
		RootPassword string `json:"root_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if req.VMName == "" || req.RootPassword == "" {
		respondError(w, http.StatusBadRequest, "vm_name y root_password son requeridos")
		return
	}

	if err := h.Platform.RepairKeys(req.VMName, req.RootPassword); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, fmt.Sprintf("Llaves reparadas en VM '%s'", req.VMName), nil)
}

// DeployRootKeysToUserVM deploys root SSH keys to an existing user VM
func (h *Handler) DeployRootKeysToUserVM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req struct {
		VMName       string `json:"vm_name"`
		RootPassword string `json:"root_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if req.VMName == "" || req.RootPassword == "" {
		respondError(w, http.StatusBadRequest, "vm_name y root_password son requeridos")
		return
	}

	if err := h.Platform.DeployRootKeysToUserVM(req.VMName, req.RootPassword); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, fmt.Sprintf("Llaves root desplegadas en VM '%s'", req.VMName), nil)
}

// DeleteUserVM removes a user VM
func (h *Handler) DeleteUserVM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}

	var req struct {
		VMName string `json:"vm_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}

	if err := h.Platform.DeleteUserVM(req.VMName); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondSuccess(w, "Máquina virtual eliminada exitosamente", nil)
}

// RegisterRoutes registers all HTTP routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/dashboard", h.GetDashboard)
	mux.HandleFunc("/api/vms/available", h.ListAvailableVMs)
	mux.HandleFunc("/api/base-vm/add", h.AddBaseVM)
	mux.HandleFunc("/api/base-vm/create-keys", h.CreateRootKeys)
	mux.HandleFunc("/api/base-vm/download-keys", h.DownloadRootKeys)
	mux.HandleFunc("/api/disk/create", h.CreateDisk)
	mux.HandleFunc("/api/disk/disconnect", h.DisconnectDisk)
	mux.HandleFunc("/api/disk/connect", h.ConnectDisk)
	mux.HandleFunc("/api/disk/delete", h.DeleteDisk)
	mux.HandleFunc("/api/user-vm/create", h.CreateUserVM)
	mux.HandleFunc("/api/user-vm/create-user", h.CreateVMUser)
	mux.HandleFunc("/api/user-vm/users", h.ListVMUsers)
	mux.HandleFunc("/api/user-vm/download-keys", h.DownloadUserKeys)
	mux.HandleFunc("/api/user-vm/delete", h.DeleteUserVM)
	mux.HandleFunc("/api/user-vm/deploy-root-keys", h.DeployRootKeysToUserVM)
	mux.HandleFunc("/api/user-vm/repair-keys", h.RepairKeys)
}
