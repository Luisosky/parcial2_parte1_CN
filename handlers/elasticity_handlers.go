package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"vm-platform/models"
	"vm-platform/services"
)

// ElasticityHandler holds elasticity-related HTTP handler dependencies
type ElasticityHandler struct {
	Elastic  *services.ElasticityService
	Monitor  *services.MonitorService
	HAProxy  *services.HAProxyService
	Platform *services.PlatformService
}

func NewElasticityHandler(elastic *services.ElasticityService, monitor *services.MonitorService, haproxy *services.HAProxyService, platform *services.PlatformService) *ElasticityHandler {
	return &ElasticityHandler{
		Elastic:  elastic,
		Monitor:  monitor,
		HAProxy:  haproxy,
		Platform: platform,
	}
}

// ─── Load balancer endpoints ───

func (h *ElasticityHandler) ListLBs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	respondSuccess(w, "Balanceadores", h.Elastic.ListLoadBalancers())
}

func (h *ElasticityHandler) CreateLB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req models.LoadBalancerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	lb, err := h.Elastic.CreateLoadBalancer(&req)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondSuccess(w, fmt.Sprintf("Balanceador '%s' creado", lb.Name), lb)
}

func (h *ElasticityHandler) UpdateLB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req models.LoadBalancerCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	if err := h.Elastic.UpdateLoadBalancer(req.Name, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondSuccess(w, "Balanceador actualizado", nil)
}

func (h *ElasticityHandler) DeleteLB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	if err := h.Elastic.DeleteLoadBalancer(req.Name); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondSuccess(w, "Balanceador eliminado", nil)
}

func (h *ElasticityHandler) InstallLB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	if err := h.Elastic.InstallAndStart(req.Name); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(w, "HAProxy instalado y en ejecución", nil)
}

func (h *ElasticityHandler) LBAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req struct {
		Name   string `json:"name"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	lb := h.Elastic.GetLoadBalancer(req.Name)
	if lb == nil {
		respondError(w, http.StatusNotFound, "LB no encontrado")
		return
	}
	if err := h.HAProxy.Action(lb, req.Action); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(w, "Acción ejecutada", nil)
}

func (h *ElasticityHandler) LBStatus(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	lb := h.Elastic.GetLoadBalancer(name)
	if lb == nil {
		respondError(w, http.StatusNotFound, "LB no encontrado")
		return
	}
	status := h.HAProxy.Status(lb)
	respondSuccess(w, "Status", map[string]interface{}{"status": status, "lb": lb})
}

// ─── Backend endpoints ───

func (h *ElasticityHandler) AddBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req models.BackendAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	if err := h.Elastic.AddBackend(&req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondSuccess(w, "Backend agregado", nil)
}

func (h *ElasticityHandler) UpdateBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req struct {
		LBName      string `json:"lb_name"`
		BackendName string `json:"backend_name"`
		Address     string `json:"address"`
		Port        int    `json:"port"`
		Weight      int    `json:"weight"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	baReq := &models.BackendAddRequest{
		Address: req.Address,
		Port:    req.Port,
		Weight:  req.Weight,
	}
	if err := h.Elastic.UpdateBackend(req.LBName, req.BackendName, baReq); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondSuccess(w, "Backend actualizado", nil)
}

func (h *ElasticityHandler) RemoveBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req struct {
		LBName      string `json:"lb_name"`
		BackendName string `json:"backend_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	if err := h.Elastic.RemoveBackend(req.LBName, req.BackendName); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondSuccess(w, "Backend removido", nil)
}

func (h *ElasticityHandler) ToggleBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req struct {
		LBName      string `json:"lb_name"`
		BackendName string `json:"backend_name"`
		Enabled     bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	if err := h.Elastic.ToggleBackend(req.LBName, req.BackendName, req.Enabled); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondSuccess(w, "Backend actualizado", nil)
}

// ─── Elasticity config ───

func (h *ElasticityHandler) UpdateElasticity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req models.ElasticityUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	if err := h.Elastic.UpdateElasticity(&req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondSuccess(w, "Configuración de elasticidad actualizada", nil)
}

// ─── Monitoring / snapshot ───

func (h *ElasticityHandler) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		respondError(w, http.StatusBadRequest, "name es requerido")
		return
	}
	snap := h.Elastic.GetSnapshot(name)
	if snap == nil {
		respondError(w, http.StatusNotFound, "LB no encontrado")
		return
	}
	respondSuccess(w, "Snapshot", snap)
}

func (h *ElasticityHandler) GetEventLog(w http.ResponseWriter, r *http.Request) {
	respondSuccess(w, "Eventos", h.Monitor.GetEventLog())
}

func (h *ElasticityHandler) GetCPUHistory(w http.ResponseWriter, r *http.Request) {
	vmName := r.URL.Query().Get("vm_name")
	if vmName == "" {
		respondError(w, http.StatusBadRequest, "vm_name es requerido")
		return
	}
	respondSuccess(w, "Historia CPU", h.Monitor.GetHistory(vmName))
}

// ─── Stress-ng ───

func (h *ElasticityHandler) StartStress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Método no permitido")
		return
	}
	var req struct {
		VMName       string `json:"vm_name"`
		Username     string `json:"username"`
		RootPassword string `json:"root_password"`
		CPUs         int    `json:"cpus"`
		LoadPercent  int    `json:"load_percent"`
		Duration     int    `json:"duration_sec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Datos inválidos")
		return
	}
	if err := h.Monitor.StartStress(req.VMName, req.Username, req.RootPassword, req.CPUs, req.LoadPercent, req.Duration); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(w, fmt.Sprintf("stress-ng iniciado en %s", req.VMName), nil)
}

func (h *ElasticityHandler) StopStress(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Monitor.StopStress(req.VMName, req.RootPassword); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(w, "stress-ng detenido", nil)
}

// ─── List helpers ───

// ListAvailableBackendVMs returns user VMs that could be used as backends
func (h *ElasticityHandler) ListAvailableBackendVMs(w http.ResponseWriter, r *http.Request) {
	data := h.Platform.GetDashboardData()
	type Entry struct {
		VMName   string `json:"vm_name"`
		Username string `json:"username"`
		DiskName string `json:"disk_name"`
	}
	var list []Entry
	for _, uvm := range data.UserVMs {
		if uvm.UserCreated {
			list = append(list, Entry{VMName: uvm.Name, Username: uvm.Username, DiskName: uvm.DiskName})
		}
	}
	for _, bvm := range data.BaseVMs {
		if bvm.KeysCreated {
			list = append(list, Entry{VMName: bvm.Name, Username: "root"})
		}
	}
	if list == nil {
		list = []Entry{}
	}
	respondSuccess(w, "VMs candidatas a backend", list)
}

func (h *ElasticityHandler) ListDisks(w http.ResponseWriter, r *http.Request) {
	data := h.Platform.GetDashboardData()
	type Entry struct {
		Name       string `json:"name"`
		BaseVMName string `json:"base_vm_name"`
	}
	var list []Entry
	for _, d := range data.Disks {
		list = append(list, Entry{Name: d.Name, BaseVMName: d.BaseVMName})
	}
	if list == nil {
		list = []Entry{}
	}
	respondSuccess(w, "Discos multiattach", list)
}

// RegisterElasticityRoutes
func (h *ElasticityHandler) RegisterRoutes(mux *http.ServeMux) {
	// LB CRUD
	mux.HandleFunc("/api/lb/list", h.ListLBs)
	mux.HandleFunc("/api/lb/create", h.CreateLB)
	mux.HandleFunc("/api/lb/update", h.UpdateLB)
	mux.HandleFunc("/api/lb/delete", h.DeleteLB)
	mux.HandleFunc("/api/lb/install", h.InstallLB)
	mux.HandleFunc("/api/lb/action", h.LBAction)
	mux.HandleFunc("/api/lb/status", h.LBStatus)
	// Backends
	mux.HandleFunc("/api/lb/backend/add", h.AddBackend)
	mux.HandleFunc("/api/lb/backend/update", h.UpdateBackend)
	mux.HandleFunc("/api/lb/backend/remove", h.RemoveBackend)
	mux.HandleFunc("/api/lb/backend/toggle", h.ToggleBackend)
	// Elasticity config
	mux.HandleFunc("/api/lb/elasticity/update", h.UpdateElasticity)
	// Snapshot / monitoring
	mux.HandleFunc("/api/lb/snapshot", h.GetSnapshot)
	mux.HandleFunc("/api/lb/events", h.GetEventLog)
	mux.HandleFunc("/api/lb/cpu-history", h.GetCPUHistory)
	// Stress-ng
	mux.HandleFunc("/api/stress/start", h.StartStress)
	mux.HandleFunc("/api/stress/stop", h.StopStress)
	// Helpers
	mux.HandleFunc("/api/lb/available-vms", h.ListAvailableBackendVMs)
	mux.HandleFunc("/api/lb/disks", h.ListDisks)
}
